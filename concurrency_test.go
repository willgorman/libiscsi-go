package iscsi_test

import (
	"context"
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
	deviceSize := 20 * MiB
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

	consumers := 4
	for i := 0; i < consumers; i++ {
		t.Log("START CONSUMER")
		go func() {
			for r := range output {
				if r.Err != nil {
					t.Fail()
					t.Log(err)
				}
				assert.Assert(t, len(r.Task.DataIn) > 0)

				time.Sleep(time.Duration(rand.Intn(500) * int(time.Millisecond)))
				wait.Done()
			}
		}()
	}
	for i, n := 0, 0; i < totalBlocks; i, n = i+blockChunk, n+1 {
		err := device.Read16Async(iscsi.Read16{
			LBA:       i,
			Blocks:    blockChunk,
			BlockSize: 512,
		}, output)
		if err != nil {
			t.Fatal(err)
		}
		wait.Add(1)
		if n%10 == 0 {
			t.Log("PROCESS")
			ctx, cancel := context.WithCancel(context.Background())
			go func() {
				wait.Wait()
				cancel()
			}()
			err = device.ProcessAsync(ctx)
			if err != nil {
				t.Fatal(err)
			}

		}
		// if i%4*MiB/512 == 0 {
		// 	t.Log("POLL AT ", i)
		// 	err = device.ProcessAsyncN(1)
		// 	if err != nil {
		// 		t.Fatal(err)
		// 	}
		// }
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	go func() {
		wait.Wait()
		cancel()
	}()
	device.ProcessAsync(ctx)
}

func BenchmarkConcurrentConsumers(b *testing.B) {
	for i := 0; i < b.N; i++ {
	}
}
