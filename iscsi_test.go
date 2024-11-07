package iscsi_test

import (
	"fmt"
	"os"
	"testing"

	"github.com/gostor/gotgt/pkg/config"
	_ "github.com/gostor/gotgt/pkg/port/iscsit"
	"github.com/gostor/gotgt/pkg/scsi"
	_ "github.com/gostor/gotgt/pkg/scsi/backingstore"
	iscsi "github.com/willgorman/libiscsi-go"
)

const (
	_ = 1 << (10 * iota)
	KiB
	MiB
	GiB
	TiB
)

func createTargetTempfile(t *testing.T, size int64) string {
	file, err := os.CreateTemp("", t.Name())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(file.Name()) })
	err = file.Truncate(size)
	if err != nil {
		t.Fatal(err)
	}
	return file.Name()
}

func TestWithGoTGT(t *testing.T) {
	imgFile := createTargetTempfile(t, 10*MiB)
	c := &config.Config{
		Storages: []config.BackendStorage{
			{
				DeviceID:         1000,
				Path:             fmt.Sprintf("file:%s", imgFile),
				Online:           true,
				ThinProvisioning: true,
			},
		},
		ISCSIPortals: []config.ISCSIPortalInfo{
			{ID: 0, Portal: "127.0.0.1:3260"},
		},
		ISCSITargets: map[string]config.ISCSITarget{
			"iqn.2024-10.com.example:0:0": {
				TPGTs: map[string][]uint64{
					"1": {0},
				},
				LUNs: map[string]uint64{
					"0": 1000,
				},
			},
		},
	}
	err := scsi.InitSCSILUMap(c)
	if err != nil {
		t.Fatal(err)
	}
	tgtsvc := scsi.NewSCSITargetService()
	targetDriver, err := scsi.NewTargetDriver("iscsi", tgtsvc)
	if err != nil {
		t.Fatal(err)
	}
	for tgtname := range c.ISCSITargets {
		err = targetDriver.NewTarget(tgtname, c)
		if err != nil {
			t.Fatal(err)
		}
	}
	go targetDriver.Run(3260)
	defer targetDriver.Close()

	device := iscsi.New(iscsi.ConnectionDetails{
		InitiatorIQN: "iqn.2024-10.libiscsi:go",
		TargetURL:    "iscsi://127.0.0.1/iqn.2024-10.com.example:0:0/0",
	})
	err = device.Connect()
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = device.Disconnect()
	}()
	capacity, err := device.ReadCapacity10()
	if err != nil {
		t.Fatal(err)
	}

	write := make([]byte, capacity.BlockSize)
	copy(write, []byte("hello!"))

	err = device.Write16(iscsi.Write16{LBA: 0, Data: write, BlockSize: capacity.BlockSize})
	if err != nil {
		t.Fatal(err)
	}

	data, err := device.Read16(iscsi.Read16{LBA: 0, Blocks: 1, BlockSize: capacity.BlockSize})
	if err != nil {
		t.Fatal(err)
	}
	t.Log("data", string(data))
}
