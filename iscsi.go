package iscsi

/*
#cgo pkg-config: libiscsi
#include <stdlib.h>
#include "iscsi/iscsi.h"
#include "iscsi/scsi-lowlevel.h"
*/
import "C"

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"log"
	"time"
	"unsafe"

	"github.com/avast/retry-go/v4"
	gopointer "github.com/mattn/go-pointer"
	"golang.org/x/sys/unix"
)

type (
	iscsiContext *C.struct_iscsi_context
)

type device struct {
	Context      iscsiContext
	targetName   string
	targetPortal string
	targetLun    int
	details      ConnectionDetails
}

type ConnectionDetails struct {
	InitiatorIQN string
	TargetURL    string
}

func New(details ConnectionDetails) *device {
	return &device{
		details: details,
	}
}

func (d *device) initializeContext() {
	if d.Context != nil {
		_ = C.iscsi_destroy_context(d.Context)
		d.Context = nil
		d.targetLun = 0
		d.targetName = ""
		d.targetPortal = ""
	}
	ctx := C.iscsi_create_context(C.CString(d.details.InitiatorIQN))
	url := C.iscsi_parse_full_url(ctx, C.CString(d.details.TargetURL))
	_ = C.iscsi_set_targetname(ctx, &url.target[0])
	defer C.iscsi_destroy_url(url)
	d.Context = ctx
	d.targetLun = int(url.lun)
	d.targetName = C.GoString(&url.target[0])
	d.targetPortal = C.GoString(&url.portal[0])
	_ = C.iscsi_set_session_type(d.Context, C.ISCSI_SESSION_NORMAL)
	_ = C.iscsi_set_header_digest(d.Context, C.ISCSI_HEADER_DIGEST_NONE_CRC32C)
}

func (d *device) Connect() error {
	d.initializeContext()
	return retry.Do(func() error {
		if retval := C.iscsi_full_connect_sync(d.Context, C.CString(d.targetPortal), C.int(d.targetLun)); retval != 0 {
			errstr := C.iscsi_get_error(d.Context)
			// reset the context before retrying.  it seems like some connection
			// errors leave the context in an inconsistent state that makes it
			// difficult to reuse
			d.initializeContext()
			return fmt.Errorf("iscsi_full_connect_sync: (%d) %s", retval, C.GoString(errstr))
		}
		return nil
	}, retry.Attempts(20), retry.MaxDelay(500*time.Millisecond))
}

func (d *device) Reconnect() error {
	if retval := C.iscsi_reconnect_sync(d.Context); retval != 0 {
		if retval != 0 {
			return fmt.Errorf("failed to reconnect with: %d", retval)
		}
	}
	return nil
}

func (d *device) Disconnect() error {
	defer C.iscsi_destroy_context(d.Context)
	retval := C.iscsi_logout_sync(d.Context)
	if retval != 0 {
		return fmt.Errorf("failed to logout with: %d", retval)
	}
	return nil
}

type capacity struct {
	LBA       int
	BlockSize int
}

func (d device) ReadCapacity10() (c capacity, err error) {
	task := C.iscsi_readcapacity10_sync(d.Context, 0, 0, 0)
	if task == nil || task.status != C.SCSI_STATUS_GOOD {
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

func (d *device) Write16(data Write16) error {
	carr := []C.uchar(string(data.Data))
	// TODO: (willgorman) figure out why larger blocksizes cause SCSI_SENSE_ASCQ_INVALID_FIELD_IN_INFORMATION_UNIT
	task := C.iscsi_write16_sync(d.Context, 0,
		C.uint64_t(data.LBA), &carr[0], C.uint(len(carr)), C.int(data.BlockSize), 0, 0, 0, 0, 0)
	if task == nil || task.status != C.SCSI_STATUS_GOOD {
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

func (d *device) Read16(data Read16) ([]byte, error) {
	task := C.iscsi_read16_sync(d.Context, 0, C.uint64_t(data.LBA),
		C.uint(data.BlockSize*data.Blocks), C.int(data.BlockSize), 0, 0, 0, 0, 0)
	if task == nil || task.status != C.SCSI_STATUS_GOOD {
		errstr := C.iscsi_get_error(d.Context)
		if C.GoString(errstr) == "Poll failed" {
			return nil, errors.New("Poll failed")
		}
		return nil, fmt.Errorf("iscsi_read16_sync: %s", C.GoString(errstr))
	}

	size := task.datain.size
	dataread := unsafe.Slice(task.datain.data, size)
	// surely there's a better way to get from []C.uchar to []byte?
	return []byte(string(dataread)), nil
}

func (d *device) Read16Async(data Read16, tasks chan TaskResult) error {
	cdata := callbackData{
		tasks: tasks,
		// add the read request so the callback can tell what lba the read
		// started at.  i suspect this is probably also in the scsi_task
		// but this is simple enough for now
		context: data,
	}
	pdata := gopointer.Save(cdata)
	// can't call unref until the callback is done
	task := C.iscsi_read16_task(d.Context, 0, C.uint64_t(data.LBA),
		C.uint(data.BlockSize*data.Blocks), C.int(data.BlockSize), 0, 0, 0, 0, 0, channelCB, pdata)
	if task == nil {
		return errors.New("unable to start iscsi_read16_task")
	}

	return nil
}

// this is not safe to run in a goroutine other than the one
// where all other operations on the iscsi connection are
// being performed
// TODO: i'm not sure this function even makes sense because
// it can't run concurrently with iscsi operations
func (d *device) ProcessAsync(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return nil
		default:
			events := d.WhichEvents()
			if events == 0 {
				time.Sleep(10 * time.Millisecond)
				continue
			}
			fd := unix.PollFd{
				Fd:      int32(d.GetFD()),
				Events:  int16(events),
				Revents: 0,
			}

			fds := []unix.PollFd{fd}
			_, err := unix.Poll(fds, 1000)
			if err != nil {
				if err.Error() != "interrupted system call" {
					log.Fatal("oh no", err)
				}
			}
			// I think we have to call this with fds[0], not fd.
			// fds[0] is what actually gets updated, fd is just a copy
			if d.HandleEvents(fds[0].Revents) < 0 {
				log.Fatal("welp idk")
			}
		}
	}
}

func (d *device) GetFD() int {
	return int(C.iscsi_get_fd(d.Context))
}

func (d *device) WhichEvents() int {
	return int(C.iscsi_which_events(d.Context))
}

func (d *device) HandleEvents(n int16) int {
	return int(C.iscsi_service(d.Context, C.int(n)))
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

// Don't want to expose C structs to callers and can't
// embed C structs in a Go struct so we need getters
type Task struct {
	Status int
	DataIn []byte
}

type callbackData struct {
	tasks   chan TaskResult
	context any
}

type TaskResult struct {
	Task    Task
	Err     error
	Context any
}

//export iscsiChannelCB
func iscsiChannelCB(iscsiCtx iscsiContext, status int, command_data, private_data unsafe.Pointer) {
	defer gopointer.Unref(private_data)
	data := gopointer.Restore(private_data).(callbackData)

	if status != C.SCSI_STATUS_GOOD {
		data.tasks <- TaskResult{
			Err: errors.New(C.GoString(C.iscsi_get_error(iscsiCtx))),
		}
		return
	}
	// get command data onto the channel
	task := (*C.struct_scsi_task)(command_data)
	defer C.free(command_data)

	data.tasks <- TaskResult{
		// TODO: (willgorman) should we copy data out of the task and free it here?
		Task: Task{
			Status: int(task.status),
			DataIn: getDataIn(task),
		},
		Context: data.context,
	}
}

func getDataIn(t *C.struct_scsi_task) []byte {
	size := t.datain.size
	dataread := unsafe.Slice(t.datain.data, size)
	// surely there's a better way to get from []C.uchar to []byte?
	return []byte(string(dataread))
}
