package iscsi

/*
#cgo pkg-config: libiscsi
#include "iscsi/iscsi.h"
#include "iscsi/scsi-lowlevel.h"

extern void iscsiChannelCB(struct iscsi_context*, int,
				 void*, void*);

extern void iscsiSyncCB(struct iscsi_context*, int,
				 void*, void*);

void iscsiChannelCB_cgo(struct iscsi_context *iscsi, int status,
				 void *command_data, void *private_data) {
  iscsiChannelCB(iscsi, status, command_data, private_data);
}

void iscsiSyncCB_cgo(struct iscsi_context *iscsi, int status,
				 void *command_data, void *private_data) {
  iscsiSyncCB(iscsi, status, command_data, private_data);
}
*/
import "C"

var channelCB = C.iscsi_command_cb(C.iscsiChannelCB_cgo)

var syncCB = C.iscsi_command_cb(C.iscsiSyncCB_cgo)
