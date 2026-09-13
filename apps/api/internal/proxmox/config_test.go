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
