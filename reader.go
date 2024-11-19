package iscsi

import (
	"fmt"
	"io"
	"log"
)

type reader struct {
	dev       *device
	lba       int64
	offset    int64
	blocksize int64
}

func Reader(dev *device) (*reader, error) {
	c, err := dev.ReadCapacity16()
	if err != nil {
		return nil, fmt.Errorf("failed to get capacity of device: %w", err)
	}
	return &reader{
		dev:       dev,
		lba:       int64(c.LBA) + 1,
		offset:    0,
		blocksize: int64(c.BlockSize),
	}, nil
}

func (r *reader) Close() error {
	return r.dev.Disconnect()
}

func (r *reader) Read(p []byte) (n int, err error) {
	if r.offset >= r.blocksize*r.lba {
		log.Printf("EOF at %d", r.blocksize*r.lba)
		return 0, io.EOF
	}
	// log.Printf("READ %d bytes from offset %d\n", len(p), r.offset)
	// find our starting lba
	startBlock := r.offset / r.blocksize
	endOffset := len(p) + int(r.offset)

	blocks := (endOffset-int(r.offset))/int(r.blocksize) + 1

	blocks = min(blocks, int(r.lba)-int(startBlock))

	// TODO: (willgorman) handle EOF.  If endOffset is > total size
	// need to make sure our block
	if (endOffset/int(r.blocksize)) > int(r.lba) || blocks == int(r.lba-startBlock) {
		// log.Println("HIT THE EOF!!!!!")
		err = io.EOF
		// endOffset = int(r.lba) * int(r.blocksize)
		// log.Println("R.LBA! ", r.lba)
		// log.Println("END OFFSET! ", endOffset)
		blocks = int(r.lba - startBlock)
	} else if endOffset%int(r.blocksize) != 0 {
		// if endoffset is not block aligned then we need to read one more block
		blocks++
	}

	blocks = min(blocks, int(r.lba)-int(startBlock))

	read16 := Read16{
		LBA:       int(startBlock),
		BlockSize: int(r.blocksize),
		Blocks:    blocks,
	}
	// log.Printf("%#v", read16)
	readBytes, readErr := r.dev.Read16(read16)
	if readErr != nil {
		return 0, fmt.Errorf("iscsi device read error: %w", readErr)
	}

	blockOffset := r.offset % r.blocksize
	// log.Printf("OFFSET %d INTO BLOCK %d\n", blockOffset, startBlock)
	// log.Printf("OFFSET + LEN(P): %d | LEN(readBytes)-OFFSET: %d", int(blockOffset)+len(p), len(readBytes)-int(blockOffset))

	var result []byte
	l := min(len(p), len(readBytes))
	if err == io.EOF {
		result = readBytes[blockOffset:]
	} else if blockOffset > 0 {
		result = readBytes[blockOffset : l+int(blockOffset)]
	} else {
		result = readBytes[:l]
	}

	// log.Printf("LEN N WILL BE: %d", len(result))
	r.offset = int64(endOffset)

	return copy(p, result), err
}

func (r *reader) ReadAt(p []byte, off int64) (n int, err error) {
	return 0, nil
}
