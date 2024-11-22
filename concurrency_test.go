package iscsi_test

import (
	"context"
	"io"
	"log/slog"
	"math/rand"
	"os"
	"sync"
	"testing"
	"time"

	iscsi "github.com/willgorman/libiscsi-go"
	"gotest.tools/assert"
)

func TestConcurrentConsumers(t *testing.T) {
	iscsi.SetLogger(slog.Default())
	seed := time.Now().UnixNano()
	t.Logf("using seed %d", seed)
	rnd := rand.New(rand.NewSource(seed))
	deviceSize := 100 * MiB
	fileName := writeTargetTempfile(t, rnd, int64(deviceSize))
	file, err := os.Open(fileName)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	device := iscsi.New(iscsi.ConnectionDetails{
		InitiatorIQN: "iqn.2024-10.libiscsi:go",
		TargetURL:    runTestTarget(t, fileName),
	})

	err = device.Connect()
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = device.Disconnect()
	}()

	output := make(chan iscsi.TaskResult, 20)
	totalBlocks := deviceSize / 512
	// read 1MiB at a time
	blockChunk := MiB / 512
	wait := sync.WaitGroup{}
	t.Log("WAITING FOR ", totalBlocks/blockChunk)
	wait.Add(totalBlocks / blockChunk)
	consumers := 10
	for i := 0; i < consumers; i++ {
		t.Log("START CONSUMER")
		go func() {
			for r := range output {
				if r.Err != nil {
					t.Fail()
					t.Log(err)
				}
				assert.Assert(t, len(r.Task.DataIn) > 0)
				// simulate a slow consumer
				// time.Sleep(time.Duration(rand.Intn(500) * int(time.Millisecond)))
				time.Sleep(100 * time.Millisecond)
				wait.Done()
			}
		}()
	}
	for i := 0; i < totalBlocks; i = i + blockChunk {
		err := device.Read16Async(iscsi.Read16{
			LBA:       i,
			Blocks:    blockChunk,
			BlockSize: 512,
		}, output)
		if err != nil {
			t.Fatal(err)
		}

		// once there are enough reads queued up,
		// take a break from reading to process
		// them and feed output to the consumers
		if device.GetQueueLength() > 10 {
			for device.GetQueueLength() > 4 {
				_ = device.ProcessAsyncN(1)
			}
		}
	}

	// handle any remaining requests in the queue
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	go func() {
		wait.Wait()
		// once all consumers are done, halt ProcessAsync
		cancel()
	}()
	device.ProcessAsync(ctx)
}

// TODO: is this right? it's really fast, maybe it's missing something
func TestParallelSyncConsumers(t *testing.T) {
	iscsi.SetLogger(slog.Default())
	seed := time.Now().UnixNano()
	t.Logf("using seed %d", seed)
	rnd := rand.New(rand.NewSource(seed))
	deviceSize := 100 * MiB
	fileName := writeTargetTempfile(t, rnd, int64(deviceSize))
	file, err := os.Open(fileName)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()

	url := runTestTarget(t, fileName)
	// iscsi.SetLogger(slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug})))

	blocks := deviceSize / 512
	for i := 0; i < blocks; i = i + (blocks / 4) {
		go func() {
			device := iscsi.New(iscsi.ConnectionDetails{
				InitiatorIQN: "iqn.2024-10.libiscsi:go",
				TargetURL:    url,
			})

			err = device.Connect()
			if err != nil {
				t.Fail()
				t.Log(err)
			}
			defer func() {
				_ = device.Disconnect()
			}()

			cap, err := device.ReadCapacity16()
			if err != nil {
				t.Fail()
				t.Log(err)
			}
			rdr, err := iscsi.RangeReader(device, i, i+(cap.LBA/4))
			if err != nil {
				t.Fail()
				t.Log(err)
			}

			wtr := delayWriter{io.Discard}
			buf := make([]byte, MiB)
			for i := 0; i < deviceSize/len(buf); i++ {
				_, err := rdr.Read(buf)
				if err != nil {
					t.Fail()
					t.Log("read err ", err)
				}
				_, err = wtr.Write(buf)
				if err != nil {
					t.Fail()
					t.Log("write err ", err)
				}
			}
		}()
	}
}

type delayWriter struct {
	io.Writer
}

func (d *delayWriter) Write(p []byte) (n int, err error) {
	time.Sleep(100 * time.Millisecond)
	return d.Writer.Write(p)
}

func BenchmarkConcurrentConsumers(b *testing.B) {
	for i := 0; i < b.N; i++ {
	}
}
