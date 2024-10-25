package iscsi

/*
#cgo pkg-config: libiscsi
#include "iscsi/iscsi.h"
#include "iscsi/scsi-lowlevel.h"
extern void read16CB(struct iscsi_context*, int,
				 void*, void*);
extern void iscsiChannelCB(struct iscsi_context*, int,
				 void*, void*);
void read16CB_cgo(struct iscsi_context *iscsi, int status,
				 void *command_data, void *private_data) {
  read16CB(iscsi, status, command_data, private_data);
}
void iscsiChannelCB_cgo(struct iscsi_context *iscsi, int status,
				 void *command_data, void *private_data) {
  iscsiChannelCB(iscsi, status, command_data, private_data);
}
*/
import "C"

var cback = C.iscsi_command_cb(C.read16CB_cgo)

var channelCB = C.iscsi_command_cb(C.iscsiChannelCB_cgo)
