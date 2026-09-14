package proxmox

import (
	"encoding/json"
	"reflect"
	"testing"
)

// qemuConfig is the shape PVE answers for a VM: device lines as strings, a
// handful of scalars as numbers, and one CD-ROM drive sitting in a disk key.
const qemuConfig = `{
	"cores": 4,
	"memory": "8192",
	"onboot": 1,
	"name": "web-01",
	"scsi0": "ceph-vm:vm-101-disk-0,iothread=1,size=32G,ssd=1",
	"scsi1": "ceph-vm:vm-101-disk-2,backup=0,size=2T",
	"scsi10": "local-lvm:vm-101-disk-4,size=100G",
	"scsi2": "/dev/disk/by-id/ata-SAMSUNG_MZ7LH1T9",
	"ide2": "local:iso/debian-13.iso,media=cdrom,size=632M",
	"ide0": "none,media=cdrom",
	"efidisk0": "ceph-vm:vm-101-disk-1,efitype=4m,pre-enrolled-keys=1,size=528K",
	"tpmstate0": "ceph-vm:vm-101-disk-3,size=4M,version=v2.0",
	"unused0": "local-lvm:vm-101-disk-9",
	"net0": "virtio=BC:24:11:00:00:01,bridge=vmbr0",
	"smbios1": "uuid=1e4d3f7a-0000-0000-0000-000000000001"
}`

// lxcConfig is the shape PVE answers for a container: rootfs and mount points
// instead of buses, and sizes written the same way.
const lxcConfig = `{
	"arch": "amd64",
	"hostname": "dns-01",
	"rootfs": "local-zfs:subvol-201-disk-0,size=8G",
	"mp0": "cephfs:subvol-201-disk-1,mp=/srv/data,backup=1,size=512G",
	"mp1": "/mnt/host/backups,mp=/mnt/backups",
	"unused0": "local-zfs:subvol-201-disk-2",
	"net0": "name=eth0,bridge=vmbr0,ip=dhcp"
}`

func TestGuestConfigUnmarshalFlattensScalars(t *testing.T) {
	var config GuestConfig
	if err := json.Unmarshal([]byte(qemuConfig), &config); err != nil {
		t.Fatalf("Unmarshal returned error: %v", err)
	}
	// PVE serialises these three differently from every device line, and a
	// caller must not have to care which form it got.
	for key, want := range map[string]string{
		"cores":  "4",
		"memory": "8192",
		"onboot": "1",
		"name":   "web-01",
	} {
		if got := config[key]; got != want {
			t.Errorf("config[%q] = %q, want %q", key, got, want)
		}
	}
}

func TestGuestConfigUnmarshalDropsNestedValues(t *testing.T) {
	var config GuestConfig
	if err := json.Unmarshal([]byte(`{"scsi0":"local:vm-1-disk-0,size=8G","weird":{"a":1},"list":[1,2]}`), &config); err != nil {
		t.Fatalf("Unmarshal returned error: %v", err)
	}
	if _, ok := config["weird"]; ok {
		t.Error("an object value was kept, want it dropped rather than mangled")
	}
	if _, ok := config["list"]; ok {
		t.Error("an array value was kept, want it dropped rather than mangled")
	}
	if got := config["scsi0"]; got == "" {
		t.Error("the device line was lost alongside the nested values")
	}
}

func TestGuestConfigDisksQemu(t *testing.T) {
	var config GuestConfig
	if err := json.Unmarshal([]byte(qemuConfig), &config); err != nil {
		t.Fatalf("Unmarshal returned error: %v", err)
	}
	disks := config.Disks()

	// Ordered by bus then by index NUMERICALLY: scsi2 before scsi10.
	wantKeys := []string{"efidisk0", "scsi0", "scsi1", "scsi2", "scsi10", "tpmstate0", "unused0"}
	gotKeys := make([]string, 0, len(disks))
	for _, disk := range disks {
		gotKeys = append(gotKeys, disk.Key)
	}
	if !reflect.DeepEqual(gotKeys, wantKeys) {
		t.Fatalf("Disks() keys = %v, want %v", gotKeys, wantKeys)
	}

	byKey := make(map[string]ConfigDisk, len(disks))
	for _, disk := range disks {
		byKey[disk.Key] = disk
	}

	if got := byKey["scsi0"]; got.Storage != "ceph-vm" || got.Volume != "ceph-vm:vm-101-disk-0" {
		t.Errorf("scsi0 = %+v, want storage ceph-vm and volume ceph-vm:vm-101-disk-0", got)
	}
	if got := byKey["scsi0"].Size; got == nil || *got != 32<<30 {
		t.Errorf("scsi0 size = %v, want 32 GiB in bytes", got)
	}
	if got := byKey["scsi1"].Size; got == nil || *got != 2<<40 {
		t.Errorf("scsi1 size = %v, want 2 TiB in bytes", got)
	}
	if got := byKey["efidisk0"].Size; got == nil || *got != 528<<10 {
		t.Errorf("efidisk0 size = %v, want 528 KiB in bytes", got)
	}
	if got := byKey["tpmstate0"].Size; got == nil || *got != 4<<20 {
		t.Errorf("tpmstate0 size = %v, want 4 MiB in bytes", got)
	}

	// A host device belongs to no storage, and its size is unknown — nil, not
	// zero, which would claim it takes no room.
	passthrough := byKey["scsi2"]
	if passthrough.Storage != "" {
		t.Errorf("scsi2 storage = %q, want empty for a host device", passthrough.Storage)
	}
	if passthrough.Size != nil {
		t.Errorf("scsi2 size = %v, want nil for a declaration carrying no size", *passthrough.Size)
	}
	if !passthrough.Attached {
		t.Error("scsi2 is attached to a bus, want Attached true")
	}

	// PVE records no size for a detached volume.
	if unused := byKey["unused0"]; unused.Attached || unused.Size != nil {
		t.Errorf("unused0 = %+v, want detached with an unknown size", unused)
	}
}

func TestGuestConfigDisksSkipsCDROM(t *testing.T) {
	var config GuestConfig
	if err := json.Unmarshal([]byte(qemuConfig), &config); err != nil {
		t.Fatalf("Unmarshal returned error: %v", err)
	}
	for _, disk := range config.Disks() {
		// ide2 holds an ISO and even declares a size; ide0 is an empty
		// drive. Neither allocates anything on a storage.
		if disk.Key == "ide2" || disk.Key == "ide0" {
			t.Errorf("Disks() returned the CD-ROM drive %q, want it skipped", disk.Key)
		}
	}
}

func TestGuestConfigDisksLXC(t *testing.T) {
	var config GuestConfig
	if err := json.Unmarshal([]byte(lxcConfig), &config); err != nil {
		t.Fatalf("Unmarshal returned error: %v", err)
	}
	disks := config.Disks()

	wantKeys := []string{"mp0", "mp1", "rootfs", "unused0"}
	gotKeys := make([]string, 0, len(disks))
	for _, disk := range disks {
		gotKeys = append(gotKeys, disk.Key)
	}
	if !reflect.DeepEqual(gotKeys, wantKeys) {
		t.Fatalf("Disks() keys = %v, want %v", gotKeys, wantKeys)
	}

	byKey := make(map[string]ConfigDisk, len(disks))
	for _, disk := range disks {
		byKey[disk.Key] = disk
	}
	if got := byKey["rootfs"].Size; got == nil || *got != 8<<30 {
		t.Errorf("rootfs size = %v, want 8 GiB in bytes", got)
	}
	if got := byKey["mp0"].Size; got == nil || *got != 512<<30 {
		t.Errorf("mp0 size = %v, want 512 GiB in bytes", got)
	}
	// A bind mount of a host directory has no storage and no size.
	if got := byKey["mp1"]; got.Storage != "" || got.Size != nil {
		t.Errorf("mp1 = %+v, want no storage and an unknown size", got)
	}
}

func TestGuestConfigDisksIgnoresNonDiskKeys(t *testing.T) {
	config := GuestConfig{
		"net0":      "virtio=BC:24:11:00:00:01,bridge=vmbr0",
		"virtiofs0": "shared,size=1G",
		"scsihw":    "virtio-scsi-single",
		"efidisk1":  "ceph-vm:vm-1-disk-0,size=528K",
		"scsi":      "ceph-vm:vm-1-disk-1,size=8G",
		"unusedx":   "local-lvm:vm-1-disk-2",
		"boot":      "order=scsi0;ide2",
	}
	if disks := config.Disks(); len(disks) != 0 {
		t.Errorf("Disks() = %+v, want none of these keys read as a volume", disks)
	}
}

func TestGuestConfigDisksNeverNil(t *testing.T) {
	// An empty list and a nil one mean different things upstream, so this
	// one is never nil: the configuration was read, it declares no volume.
	if disks := (GuestConfig{}).Disks(); disks == nil {
		t.Error("Disks() = nil on an empty configuration, want an empty slice")
	}
}

func TestParseConfigSize(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  uint64
		ok    bool
	}{
		{"bare bytes", "1048576", 1 << 20, true},
		{"kibibytes", "528K", 528 << 10, true},
		{"mebibytes", "4M", 4 << 20, true},
		{"gibibytes", "32G", 32 << 30, true},
		{"tebibytes", "2T", 2 << 40, true},
		{"pebibytes", "1P", 1 << 50, true},
		{"lowercase suffix", "8g", 8 << 30, true},
		{"spaces", " 16G ", 16 << 30, true},
		{"fraction", "1.5G", 1536 << 20, true},
		{"zero", "0", 0, true},
		{"empty", "", 0, false},
		{"suffix only", "G", 0, false},
		{"not a number", "big", 0, false},
		{"negative", "-8G", 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := parseConfigSize(tc.input)
			if ok != tc.ok {
				t.Fatalf("parseConfigSize(%q) ok = %v, want %v", tc.input, ok, tc.ok)
			}
			if ok && got != tc.want {
				t.Errorf("parseConfigSize(%q) = %d, want %d", tc.input, got, tc.want)
			}
		})
	}
}

// qemuNetConfig carries every QEMU net shape that matters: the shorthand PVE
// actually writes, an explicit model= with a separate macaddr=, a card wired
// to nothing, a VLAN tag, and an index above nine to catch string ordering.
const qemuNetConfig = `{
	"net0": "virtio=BC:24:11:AA:BB:CC,bridge=vmbr0,firewall=1",
	"net2": "model=e1000,macaddr=BC:24:11:AA:BB:DD,bridge=vmbr1,tag=120,mtu=9000",
	"net10": "virtio=BC:24:11:AA:BB:EE,link_down=1",
	"net1": "virtio=bc:24:11:aa:bb:ff,bridge=vnet-adm,tag=42",
	"scsi0": "ceph-vm:vm-101-disk-0,size=32G",
	"name": "web-01"
}`

func TestGuestConfigNetsQemu(t *testing.T) {
	var config GuestConfig
	if err := json.Unmarshal([]byte(qemuNetConfig), &config); err != nil {
		t.Fatalf("Unmarshal returned error: %v", err)
	}
	nets := config.Nets()

	// Ordered by index NUMERICALLY: net2 before net10. And "name", which is a
	// guest property rather than a card, must not be read as one.
	wantKeys := []string{"net0", "net1", "net2", "net10"}
	gotKeys := make([]string, 0, len(nets))
	for _, net := range nets {
		gotKeys = append(gotKeys, net.Key)
	}
	if !reflect.DeepEqual(gotKeys, wantKeys) {
		t.Fatalf("Nets() keys = %v, want %v", gotKeys, wantKeys)
	}

	byKey := make(map[string]ConfigNet, len(nets))
	for _, net := range nets {
		byKey[net.Key] = net
	}

	// The shorthand: the model is the KEY of the pair and the MAC its value.
	if got := byKey["net0"]; got.Model != "virtio" || got.MAC != "BC:24:11:AA:BB:CC" || got.Bridge != "vmbr0" {
		t.Errorf("net0 = %+v, want model virtio, MAC BC:24:11:AA:BB:CC on vmbr0", got)
	}
	if got := byKey["net0"]; got.Tag != nil {
		t.Errorf("net0 tag = %v, want nil: an untagged card has no VLAN, not VLAN zero", *got.Tag)
	}
	// A VM never names the interface its guest will see.
	if got := byKey["net0"]; got.Name != "" {
		t.Errorf("net0 name = %q, want empty: QEMU declares no guest-side name", got.Name)
	}
	// The long form, which a hand-edited configuration may carry.
	if got := byKey["net2"]; got.Model != "e1000" || got.MAC != "BC:24:11:AA:BB:DD" {
		t.Errorf("net2 = %+v, want model e1000 and MAC BC:24:11:AA:BB:DD", got)
	}
	if got := byKey["net2"]; got.Tag == nil || *got.Tag != 120 {
		t.Errorf("net2 tag = %v, want 120", got.Tag)
	}
	// A card attached to nothing is still a card: dropping the line would
	// hide an interface the guest has.
	if got := byKey["net10"]; got.Bridge != "" || got.MAC != "BC:24:11:AA:BB:EE" {
		t.Errorf("net10 = %+v, want no bridge and MAC BC:24:11:AA:BB:EE", got)
	}
	// PVE writes MACs uppercase; a lowercase one read back must not produce a
	// second spelling of the same address.
	if got := byKey["net1"]; got.MAC != "BC:24:11:AA:BB:FF" {
		t.Errorf("net1 MAC = %q, want it upper-cased", got.MAC)
	}
}

// lxcNetConfig is the OTHER syntax under the same key: name= is mandatory, the
// MAC hides under hwaddr=, and there is no model at all.
const lxcNetConfig = `{
	"net0": "name=eth0,bridge=vmbr0,hwaddr=BC:24:11:11:22:33,ip=dhcp,type=veth",
	"net1": "name=eth1,bridge=vnet-adm,hwaddr=BC:24:11:11:22:44,tag=42,ip=10.0.0.4/24,gw=10.0.0.1,type=veth",
	"rootfs": "local-zfs:subvol-101-disk-0,size=8G"
}`

func TestGuestConfigNetsLXC(t *testing.T) {
	var config GuestConfig
	if err := json.Unmarshal([]byte(lxcNetConfig), &config); err != nil {
		t.Fatalf("Unmarshal returned error: %v", err)
	}
	nets := config.Nets()
	if len(nets) != 2 {
		t.Fatalf("Nets() returned %d cards, want 2", len(nets))
	}

	// A container DOES name the interface its guest sees, which a VM never
	// does: the field must survive the shared parser.
	if got := nets[0]; got.Name != "eth0" || got.MAC != "BC:24:11:11:22:33" || got.Bridge != "vmbr0" {
		t.Errorf("net0 = %+v, want name eth0, MAC from hwaddr, bridge vmbr0", got)
	}
	if got := nets[0]; got.Model != "" {
		t.Errorf("net0 model = %q, want empty: a veth pair has no card model", got.Model)
	}
	if got := nets[1]; got.Tag == nil || *got.Tag != 42 {
		t.Errorf("net1 tag = %v, want 42", got.Tag)
	}
	// type=veth and ip=10.0.0.4/24 are options, not a model shorthand.
	if got := nets[1]; got.Model != "" {
		t.Errorf("net1 model = %q, want empty: no option may be mistaken for a card model", got.Model)
	}
}
