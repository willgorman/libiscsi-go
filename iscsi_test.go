package iscsi_test

import (
	"fmt"
	"os"
	"testing"

	"github.com/gostor/gotgt/pkg/config"
	_ "github.com/gostor/gotgt/pkg/port/iscsit"
	"github.com/gostor/gotgt/pkg/scsi"
	_ "github.com/gostor/gotgt/pkg/scsi/backingstore"
	"github.com/hashicorp/consul/sdk/freeport"
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

// runTestTarget creates a sparse file backed iscsi target of the given
// size and runs an iscsi server on a random ephmeral port.  The url
// returned can be used by iscsi.ConnectionDetails
func runTestTarget(t *testing.T, size int64) (url string) {
	imgFile := createTargetTempfile(t, size)
	port := freeport.GetOne(t)
	targetIQN := "iqn.2024-10.com.example:0:0"
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
			{ID: 0, Portal: fmt.Sprintf("127.0.0.1:%d", port)},
		},
		ISCSITargets: map[string]config.ISCSITarget{
			targetIQN: {
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
	go targetDriver.Run(port)
	t.Cleanup(func() { _ = targetDriver.Close() })
	return fmt.Sprintf("iscsi://127.0.0.1:%d/%s/0", port, targetIQN)
}

func TestWithGoTGT(t *testing.T) {
	device := iscsi.New(iscsi.ConnectionDetails{
		InitiatorIQN: "iqn.2024-10.libiscsi:go",
		TargetURL:    runTestTarget(t, 10*MiB),
	})
	err := device.Connect()
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
