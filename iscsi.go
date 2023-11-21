package iscsi

/*
#cgo CFLAGS: -g -Wall
#cgo LDFLAGS: -L/opt/homebrew/lib -liscsi
#include "/opt/homebrew/Cellar/libiscsi/1.19.0/include/iscsi/iscsi.h"
#include "/opt/homebrew/Cellar/libiscsi/1.19.0/include/iscsi/scsi-lowlevel.h"
*/
import "C"

import (
	"encoding/binary"
	"errors"
	"fmt"
	"time"
	"unsafe"

	"github.com/avast/retry-go/v4"
)

type (
	iscsiContext *C.struct_iscsi_context
)

type device struct {
	Context      iscsiContext
	targetName   string
	targetPortal string
	targetLun    int
}

type ConnectionDetails struct {
	InitiatorIQN string
	TargetURL    string
}

func New(details ConnectionDetails) device {
	ctx := C.iscsi_create_context(C.CString(details.InitiatorIQN))
	url := C.iscsi_parse_full_url(ctx, C.CString(details.TargetURL))
	_ = C.iscsi_set_targetname(ctx, &url.target[0])
	defer C.iscsi_destroy_url(url)
	return device{
		Context:      ctx,
		targetName:   C.GoString(&url.target[0]),
		targetPortal: C.GoString(&url.portal[0]),
		targetLun:    int(url.lun),
	}
}

func (d device) Connect() error {
	_ = C.iscsi_set_session_type(d.Context, C.ISCSI_SESSION_NORMAL)
	_ = C.iscsi_set_header_digest(d.Context, C.ISCSI_HEADER_DIGEST_NONE_CRC32C)

	return retry.Do(func() error {
		if retval := C.iscsi_full_connect_sync(d.Context, C.CString(d.targetPortal), C.int(d.targetLun)); retval != 0 {
			// it appears that sometimes a connection can partially succeed such that we
			// get an error retval but on a retry attempt get an error that we are already logged in.
			// try logging out just to see if that helps with retries
			_ = C.iscsi_logout_sync(d.Context)
			errstr := C.iscsi_get_error(d.Context)
			return fmt.Errorf("iscsi_full_connect_sync: (%d) %s", retval, C.GoString(errstr))
		}
		return nil
	}, retry.Attempts(20), retry.MaxDelay(500*time.Millisecond))
}

func (d device) Disconnect() error {
	retval := C.iscsi_logout_sync(d.Context)
	if retval != 0 {
		return fmt.Errorf("failed to logout with: %d", retval)
	}
	_ = C.iscsi_destroy_context(d.Context)
	return nil
}

type capacity struct {
	LBA       int
	BlockSize int
}

func (d device) ReadCapacity10() (c capacity, err error) {
	task := C.iscsi_readcapacity10_sync(d.Context, 0, 0, 0)
	if task == nil {
		errstr := C.iscsi_get_error(d.Context)
		return c, fmt.Errorf("iscsi_readcapacity10_sync: %s", C.GoString(errstr))
	}
	readcapacity, err := getReadCapacity(*task)
	if err != nil {
		return c, err
	}
	c.BlockSize = int(readcapacity.block_size)
	c.LBA = int(readcapacity.lba)
	return c, nil
}

type Write16 struct {
	LBA       int
	Data      []byte
	BlockSize int
}

func (d device) Write16(data Write16) error {
	carr := []C.uchar(string(data.Data))
	// TODO: (willgorman) figure out why larger blocksizes cause SCSI_SENSE_ASCQ_INVALID_FIELD_IN_INFORMATION_UNIT
	task := C.iscsi_write16_sync(d.Context, 0,
		C.ulonglong(data.LBA), &carr[0], C.uint(len(carr)), C.int(data.BlockSize), 0, 0, 0, 0, 0)
	if task == nil {
		// TODO: (willgorman) robust error checking of condition, sense key, etc
		// from libiscsi
		// ok = task->status == SCSI_STATUS_GOOD ||
		// (task->status == SCSI_STATUS_CHECK_CONDITION &&
		//  task->sense.key == SCSI_SENSE_ILLEGAL_REQUEST &&
		//  task->sense.ascq == SCSI_SENSE_ASCQ_INVALID_FIELD_IN_INFORMATION_UNIT);
		errstr := C.iscsi_get_error(d.Context)
		return fmt.Errorf("iscsi_write16_sync: %s", C.GoString(errstr))
	}
	return nil
}

type Read16 struct {
	LBA       int
	Blocks    int
	BlockSize int
}

func (d device) Read16(data Read16) ([]byte, error) {
	task := C.iscsi_read16_sync(d.Context, 0, C.ulonglong(data.LBA),
		C.uint(data.BlockSize*data.Blocks), C.int(data.BlockSize), 0, 0, 0, 0, 0)
	if task == nil {
		errstr := C.iscsi_get_error(d.Context)
		return nil, fmt.Errorf("iscsi_read16_sync: %s", C.GoString(errstr))
	}
	size := task.datain.size
	dataread := unsafe.Slice(task.datain.data, size)
	// surely there's a better way to get from []C.uchar to []byte?
	return []byte(string(dataread)), nil
}

func getReadCapacity(task C.struct_scsi_task) (C.struct_scsi_readcapacity10, error) {
	cap := C.struct_scsi_readcapacity10{}

	if task.cdb[0] != C.SCSI_OPCODE_READCAPACITY10 {
		return cap, errors.New("unexpected opcode")
	}
	if task.datain.size != 8 {
		return cap, errors.New("unexpected size")
	}

	dataread := unsafe.Slice(task.datain.data, task.datain.size)
	databytes := []byte(string(dataread))
	cap.lba = C.uint(binary.BigEndian.Uint32(databytes[:4]))
	cap.block_size = C.uint(binary.BigEndian.Uint32(databytes[4:]))
	return cap, nil
}
