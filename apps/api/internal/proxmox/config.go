package proxmox

// This file holds the guest configuration endpoint and the parsing of the one
// thing moxy reads from it: the volumes a guest allocates.
//
// It is separate from types.go because a configuration is not a wire type with
// fields. It is an open-ended map of "key: value" lines whose values are
// themselves comma-separated property lists, in a syntax PVE shares with its
// command line and documents nowhere as a grammar. The traps it sets are
// gathered on GuestConfig.Disks.

import (
	"encoding/json"
	"math"
	"sort"
	"strconv"
	"strings"
)

// GuestConfig is /nodes/{node}/{kind}/{vmid}/config, the DECLARED
// configuration of a guest, as opposed to what it is doing right now.
//
// It is a map rather than a struct because its keys are open-ended: a guest
// carries one key per virtual device, numbered (scsi0, scsi1, net0, mp3), and
// a struct would have to enumerate the thirty-one SCSI slots PVE allows just
// to read the two a guest uses.
//
// TRAP. The values are not all JSON strings. PVE serialises "cores", "memory"
// and "sockets" as numbers while every device line is a string, and the form
// varies with the version — the same reason FlexInt exists. UnmarshalJSON
// therefore flattens scalars to their textual form, so a caller reads one type.
type GuestConfig map[string]string

// UnmarshalJSON decodes a configuration object, turning numbers and booleans
// into the strings the rest of this type assumes. A key whose value is an
// object or an array is DROPPED rather than mangled into a shape no caller
// could parse: nothing in a guest configuration is nested today, and a future
// key that is would be a new trap to read deliberately, not one to half-decode
// here.
func (c *GuestConfig) UnmarshalJSON(data []byte) error {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	out := make(GuestConfig, len(raw))
	for key, value := range raw {
		var text string
		if err := json.Unmarshal(value, &text); err == nil {
			out[key] = text
			continue
		}
		var number json.Number
		if err := json.Unmarshal(value, &number); err == nil {
			out[key] = number.String()
			continue
		}
		var flag bool
		if err := json.Unmarshal(value, &flag); err == nil {
			out[key] = strconv.FormatBool(flag)
		}
	}
	*c = out
	return nil
}

// ConfigDisk is one volume declared by a guest configuration.
//
// It is what the guest ALLOCATES, not what it consumes: PVE only knows the
// latter when a guest agent reports it, which is why the "disk" of the status
// endpoint is so often zero.
type ConfigDisk struct {
	// Key is the configuration key the volume is declared under: "scsi0",
	// "virtio1", "rootfs", "mp0", or "unused2" for a detached one.
	Key string
	// Volume is the declaration as PVE stores it: a volume id
	// ("local-lvm:vm-101-disk-0"), or a host path for a passed-through
	// device ("/dev/sdb").
	Volume string
	// Storage is the storage the volume lives on, empty for a host device,
	// which belongs to no storage at all.
	Storage string
	// Size is the declared size in BYTES, nil when the declaration carries
	// none — a passed-through device, or a detached volume, for which PVE
	// records no size. Nil means UNKNOWN, never zero.
	Size *uint64
	// Attached reports whether the volume is wired to a bus. A detached one
	// (an "unused" key) still occupies its storage but the guest cannot see
	// it, so it is listed apart rather than summed in silence.
	Attached bool
}

// Disks lists the volumes of a configuration, ordered by key so that two reads
// of the same guest do not reshuffle the list under the reader — Go randomises
// map iteration, and PVE sends no order of its own.
//
// TRAPS, all of them earned:
//
//   - A CD-ROM drive occupies a disk key without allocating anything:
//     "ide2: local:iso/debian-13.iso,media=cdrom", and "ide2: none,media=cdrom"
//     for an empty one. The discriminant is the media option, not the key.
//   - Sizes here are NOT in bytes, unlike everywhere else in this API. They
//     carry a binary suffix: "size=32G", "size=528K", "size=2T". See
//     parseConfigSize.
//   - An "unused" volume carries no size at all. Its size is unknown, which is
//     not the same as nothing.
//   - The key set is the union of both guest kinds. They do not collide — QEMU
//     numbers its buses, LXC names rootfs and mp0..255 — so this needs no kind
//     to do its job.
func (c GuestConfig) Disks() []ConfigDisk {
	out := make([]ConfigDisk, 0, 4)
	for key, value := range c {
		attached, ok := diskKeyKind(key)
		if !ok {
			continue
		}
		disk, ok := parseConfigDisk(key, value, attached)
		if !ok {
			continue
		}
		out = append(out, disk)
	}
	sort.Slice(out, func(i, j int) bool { return lessDiskKey(out[i].Key, out[j].Key) })
	return out
}

// diskDeviceKeys are the volume keys that carry an index: the QEMU buses, the
// LXC mount points, and the slot detached volumes are parked in.
var diskDeviceKeys = []string{"ide", "sata", "scsi", "virtio", "mp", "unused"}

// diskSingletonKeys are the volume keys that carry no index.
//
// efidisk0 and tpmstate0 end in a digit but admit no other index: they are
// listed here so that "efidisk1" is not read as a volume PVE would never
// write.
var diskSingletonKeys = []string{"rootfs", "efidisk0", "tpmstate0"}

// diskKeyKind reports whether a configuration key declares a volume, and
// whether that volume is attached to a bus.
//
// efidisk0 and tpmstate0 count. They are tiny — 528 KiB and 4 MiB — but they
// are real volumes on a real storage, and excluding them would be an exception
// to explain rather than a rule to apply.
func diskKeyKind(key string) (attached, ok bool) {
	for _, name := range diskSingletonKeys {
		if key == name {
			return true, true
		}
	}
	for _, prefix := range diskDeviceKeys {
		if !strings.HasPrefix(key, prefix) {
			continue
		}
		if index := key[len(prefix):]; !isDigits(index) {
			continue
		}
		return prefix != "unused", true
	}
	return false, false
}

// parseConfigDisk reads one "volume,option=value,..." declaration. It returns
// ok=false for a declaration that allocates nothing: a CD-ROM drive, empty or
// holding an ISO.
func parseConfigDisk(key, value string, attached bool) (ConfigDisk, bool) {
	volume, options, _ := strings.Cut(value, ",")
	volume = strings.TrimSpace(volume)
	if volume == "" || volume == "none" {
		return ConfigDisk{}, false
	}

	disk := ConfigDisk{Key: key, Volume: volume, Attached: attached}
	// A volume id is "storage:path"; a host device is an absolute path, which
	// belongs to no storage and must not be split on a colon it may contain.
	if !strings.HasPrefix(volume, "/") {
		if storage, _, found := strings.Cut(volume, ":"); found {
			disk.Storage = storage
		}
	}

	for _, option := range strings.Split(options, ",") {
		name, raw, found := strings.Cut(option, "=")
		if !found {
			continue
		}
		switch strings.TrimSpace(name) {
		case "media":
			if strings.TrimSpace(raw) == "cdrom" {
				return ConfigDisk{}, false
			}
		case "size":
			if size, ok := parseConfigSize(raw); ok {
				disk.Size = &size
			}
		}
	}
	return disk, true
}

// parseConfigSize reads a size the way a guest configuration writes one: a
// number with an optional binary suffix, "32G" being 32 GiB and a bare number
// being bytes. The suffixes are 1024-based, as PVE's own parser makes them.
//
// It returns ok=false rather than zero for anything it cannot read: an
// unreadable size is UNKNOWN, and reporting it as zero would claim a volume
// takes no room.
func parseConfigSize(raw string) (uint64, bool) {
	text := strings.TrimSpace(raw)
	if text == "" {
		return 0, false
	}
	multiplier := uint64(1)
	switch text[len(text)-1] {
	case 'K', 'k':
		multiplier = 1 << 10
	case 'M', 'm':
		multiplier = 1 << 20
	case 'G', 'g':
		multiplier = 1 << 30
	case 'T', 't':
		multiplier = 1 << 40
	case 'P', 'p':
		multiplier = 1 << 50
	}
	if multiplier > 1 {
		text = text[:len(text)-1]
	}
	value, err := strconv.ParseFloat(text, 64)
	if err != nil || value < 0 || math.IsInf(value, 0) || math.IsNaN(value) {
		return 0, false
	}
	// The float is there for the fractional size a hand-edited configuration
	// may carry ("size=1.5G"); the result is still a whole number of bytes.
	size := value * float64(multiplier)
	if size >= math.MaxUint64 {
		return 0, false
	}
	return uint64(size), true
}

// lessDiskKey orders two volume keys the way a reader expects them: by bus
// name, then by index NUMERICALLY, so that scsi2 comes before scsi10 — which a
// plain string comparison gets backwards.
func lessDiskKey(a, b string) bool {
	aName, aIndex := splitDiskKey(a)
	bName, bIndex := splitDiskKey(b)
	if aName != bName {
		return aName < bName
	}
	return aIndex < bIndex
}

// splitDiskKey cuts a volume key into its bus name and its index. A key with
// no index sorts as index zero, which is where its single volume belongs.
func splitDiskKey(key string) (string, int) {
	cut := len(key)
	for cut > 0 && key[cut-1] >= '0' && key[cut-1] <= '9' {
		cut--
	}
	index, err := strconv.Atoi(key[cut:])
	if err != nil {
		return key, 0
	}
	return key[:cut], index
}

// isDigits reports whether s is one or more decimal digits and nothing else.
func isDigits(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return s != ""
}
