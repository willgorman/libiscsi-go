package iscsi

import (
	"fmt"
	"io"
	"log/slog"
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
		logger().Debug("reader already at EOF", slog.Int("offset", int(r.offset)))
		return 0, io.EOF
	}
	logger().Debug("Read", slog.Int("bytes", len(p)), slog.Int("offset", int(r.offset)))

	// find our starting lba
	startBlock := r.offset / r.blocksize
	endOffset := len(p) + int(r.offset)
	blocks := (endOffset-int(r.offset))/int(r.blocksize) + 1
	blocks = min(blocks, int(r.lba)-int(startBlock))

	// handle EOF
	if (endOffset/int(r.blocksize)) > int(r.lba) || blocks == int(r.lba-startBlock) {
		err = io.EOF
		logger().Debug("reached EOF", slog.Int("lba", int(r.lba)), slog.Int("endOffset", endOffset))
		blocks = int(r.lba - startBlock)
	} else if endOffset%int(r.blocksize) != 0 {
		// if endoffset is not block aligned then we need to read one more block
		blocks++
	}

	blocks = min(blocks, int(r.lba)-int(startBlock))
	readBytes, readErr := r.dev.Read16(Read16{
		LBA:       int(startBlock),
		BlockSize: int(r.blocksize),
		Blocks:    blocks,
	})
	if readErr != nil {
		return 0, fmt.Errorf("iscsi device read error: %w", readErr)
	}

	blockOffset := r.offset % r.blocksize
	logger().Debug(fmt.Sprintf("offset %d into block %d", blockOffset, startBlock))

	var result []byte
	l := min(len(p), len(readBytes))
	if err == io.EOF {
		result = readBytes[blockOffset:]
	} else if blockOffset > 0 {
		// sometimes we get fewer than the number of requested blocks
		// even when not near the max lba? (at least when testing with gotgt)
		// unclear yet if this is acceptable for iscsi or a flaw in gotgt
		// make sure not to overshoot length of readBytes in that case
		result = readBytes[blockOffset:min(l+int(blockOffset), len(readBytes))]
	} else {
		result = readBytes[:l]
	}

	logger().Debug("finished read", slog.Int("length", len(result)))
	r.offset = int64(endOffset)

	return copy(p, result), err
}

func (r *reader) ReadAt(p []byte, off int64) (n int, err error) {
	return 0, nil
}
