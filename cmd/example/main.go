package main

import (
	"context"
	"log"
	"os"

	"github.com/sanity-io/litter"
	iscsi "github.com/willgorman/libiscsi-go"
)

func main() {
	if len(os.Args) != 3 {
		panic("missing required args")
	}
	device := iscsi.New(iscsi.ConnectionDetails{
		InitiatorIQN: os.Args[1],
		TargetURL:    os.Args[2],
	})

	err := device.Connect()
	if err != nil {
		log.Fatalln(err)
	}

	defer func() {
		_ = device.Disconnect()
	}()

	capacity, err := device.ReadCapacity10()
	if err != nil {
		log.Fatalln(err)
	}

	data := []byte("hello iscsi")
	// TODO: (willgorman) handle data > block size or just let it truncate?
	if len(data) < capacity.BlockSize {
		dataCopy := make([]byte, capacity.BlockSize)
		copy(dataCopy, data)
		data = dataCopy
	}

	litter.Dump(string(data))

	err = device.Write16(iscsi.Write16{
		LBA:       0,
		Data:      data,
		BlockSize: capacity.BlockSize,
	})
	if err != nil {
		log.Fatalln(err)
	}
	// FIXME: (willgorman) figure out timing issues.  it doesn't work right without
	// the sleeps which seems bad

	// go func() {
	// 	for {
	// 		events := device.WhichEvents()
	// 		if events == 0 {
	// 			time.Sleep(1 * time.Second)
	// 			continue
	// 		}
	// 		log.Println("got events ", events)
	// 		fd := unix.PollFd{
	// 			Fd:      int32(device.GetFD()),
	// 			Events:  int16(events),
	// 			Revents: 0,
	// 		}

	// 		fds := []unix.PollFd{fd}
	// 		log.Println("polling ", fds[0])
	// 		_, err := unix.Poll(fds, -1)
	// 		if err != nil {
	// 			log.Fatal("oh no", err)
	// 		}
	// 		log.Println("polled ", fds[0])
	// 		// I think we have to call this with fds[0], not fd.
	// 		// fds[0] is what actually gets updated, fd is just a copy
	// 		if device.HandleEvents(fds[0].Revents) < 0 {
	// 			log.Fatal("welp idk")
	// 		}
	// 		time.Sleep(1 * time.Second)
	// 	}
	// }()
	go device.ProcessAsync(context.Background())
	tasks := make(chan iscsi.TaskResult)
	for i := 0; i < 3; i++ {
		err = device.Read16Async(iscsi.Read16{
			LBA:       0,
			Blocks:    1,
			BlockSize: 512,
		}, tasks)
		if err != nil {
			log.Fatalln(err)
		}
	}

	for i := 0; i < 3; i++ {
		result := <-tasks
		if result.Err != nil {
			log.Fatal("task err ", result.Err)
		}
		readBytes := result.Task.GetDataIn()
		log.Println("read ", len(readBytes), " bytes")
		litter.Dump(result.Context)
	}
	close(tasks)

	// _, _ = device.Read16(iscsi.Read16{
	// 	LBA:       0,
	// 	Blocks:    1,
	// 	BlockSize: 512,
	// })
	// if err != nil {
	// 	log.Fatalln(err)
	// }
	// litter.Dump("hey!", string(dataread))
}
