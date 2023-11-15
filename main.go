package main

/*
#cgo CFLAGS: -g -Wall
#cgo LDFLAGS: -L/opt/homebrew/lib -liscsi
#include "/opt/homebrew/Cellar/libiscsi/1.19.0/include/iscsi/iscsi.h"
#include "/opt/homebrew/Cellar/libiscsi/1.19.0/include/iscsi/scsi-lowlevel.h"
*/
import "C"

import (
	"fmt"
	"log"
	"os"
	"unsafe"

	"github.com/sanity-io/litter"
)

type (
	ISCSIStruct C.struct_iscsi_context
	ISCSIUrl    C.struct_iscsi_url
)

func main() {
	if len(os.Args) != 3 {
		panic("missing required args")
	}
	os.Exit(playground())
}

func playground() int {
	initiator := os.Args[1]
	target := os.Args[2]
	ctx := C.iscsi_create_context(C.CString(initiator))
	defer C.iscsi_destroy_context(ctx)
	fmt.Println(C.iscsi_set_timeout(ctx, 30))

	url := C.iscsi_parse_full_url(ctx, C.CString(target))
	defer C.iscsi_destroy_url(url)

	// https://groups.google.com/g/golang-nuts/c/5IBOJnqi0Lg?pli=1
	// need to pass a pointer to the first element of the array
	fmt.Println(C.GoString(&url.target[0]))
	fmt.Println(C.GoString(&url.portal[0]))

	_ = C.iscsi_set_targetname(ctx, &url.target[0])

	// TODO: (willgorman) how to use enums from the header?
	_ = C.iscsi_set_session_type(ctx, 2)

	_ = C.iscsi_set_header_digest(ctx, 1)

	if retval := C.iscsi_full_connect_sync(ctx, &url.portal[0], url.lun); retval != 0 {
		errstr := C.iscsi_get_error(ctx)
		log.Printf("iscsi_full_connect_sync: (%d) %s", retval, C.GoString(errstr))
		return int(retval)
	}

	defer func() {
		retval := C.iscsi_logout_sync(ctx)
		if retval != 0 {
			log.Printf("failed to logout: %d", retval)
		}
	}()

	// iscsi_write16_sync(struct iscsi_context *iscsi, int lun, uint64_t lba,
	// 	unsigned char *data, uint32_t datalen, int blocksize,
	// 	int wrprotect, int dpo, int fua, int fua_nv, int group_number);

	// TODO: (willgorman) can we get the blocksize and lba count from the device?
	data := toCArray([]byte("goodbye iscsi"))
	litter.Dump(string(data))
	// TODO: (willgorman) figure out why larger blocksizes cause SCSI_SENSE_ASCQ_INVALID_FIELD_IN_INFORMATION_UNIT
	if task := C.iscsi_write16_sync(ctx, 0, 1, &data[0], 512, 512, 0, 0, 0, 0, 0); task != nil {
		// from libiscsi
		// ok = task->status == SCSI_STATUS_GOOD ||
		// (task->status == SCSI_STATUS_CHECK_CONDITION &&
		//  task->sense.key == SCSI_SENSE_ILLEGAL_REQUEST &&
		//  task->sense.ascq == SCSI_SENSE_ASCQ_INVALID_FIELD_IN_INFORMATION_UNIT);
		litter.Dump(task.status)
		litter.Dump(task.sense.ascq)
		litter.Dump(task.sense.key)
		// TODO: (willgorman) better error handling/reporting
	} else {
		errstr := C.iscsi_get_error(ctx)
		log.Printf("iscsi_write16_sync: %s", C.GoString(errstr))
		return -1
	}

	// TODO: (willgorman) read the data back
	// iscsi_read16_sync(struct iscsi_context *iscsi, int lun, uint64_t lba,
	// 	uint32_t datalen, int blocksize,
	// 	int rdprotect, int dpo, int fua, int fua_nv, int group_number)
	if task := C.iscsi_read16_sync(ctx, 0, 1, 512, 512, 0, 0, 0, 0, 0); task != nil {
		size := task.datain.size
		dataread := unsafe.Slice(task.datain.data, size)
		litter.Dump(string(dataread))

	} else {
		errstr := C.iscsi_get_error(ctx)
		log.Printf("iscsi_read16_sync: %s", C.GoString(errstr))
		return -1
	}
	return 0
}

// FIXME: (willgorman) this can't possibly be the best way
func toCArray(bs []byte) []C.uchar {
	// TODO: (willgorman) validate len(bs) <= 512
	ret := make([]C.uchar, 512)
	for i, b := range bs {
		ret[i] = C.uchar(b)
	}
	return ret
}
