package iscsi

/*
#cgo pkg-config: libiscsi
#include "iscsi/iscsi.h"
#include "iscsi/scsi-lowlevel.h"
extern void read16CB(struct iscsi_context*, int,
				 void*, void*);
void read16CBCgo1(struct iscsi_context *iscsi, int status,
				 void *command_data, void *private_data) {
  read16CB(iscsi, status, command_data, private_data);
}
*/
import "C"

// var read16Callback = C.read16CBCgo1
var cback = C.iscsi_command_cb(C.read16CBCgo1)
