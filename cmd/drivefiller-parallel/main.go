package main

import (
	"crypto/rand"
	"flag"
	"log"
	"os"
	"sync"

	iscsi "github.com/willgorman/libiscsi-go"
)

const (
	MiB = int64(1024 * 1024)
	GiB = int64(1024 * MiB)
)

var (
	maxConcurrency int
	writeBlocks    int
	targetURL      string
	initiator      string
)

func main() {
	flag.StringVar(&targetURL, "target", "", "target URL")
	flag.StringVar(&initiator, "init", "", "initiator")
	flag.IntVar(&maxConcurrency, "max-workers", 50, "max number of threads")
	flag.IntVar(&writeBlocks, "write-size", 100, "blocks to write in each write")
	flag.Parse()

	if targetURL == "" || initiator == "" {
		log.Fatal("target and init required")
	}

	device := iscsi.New(iscsi.ConnectionDetails{
		InitiatorIQN: initiator,
		TargetURL:    targetURL,
	})

	n := int64(1000)

	err := device.Connect()
	if err != nil {
		log.Fatalln(err)
	}
	log.Println("connected")

	defer func() {
		_ = device.Disconnect()
	}()

	capacity, err := device.ReadCapacity10()
	if err != nil {
		log.Fatalln(err)
	}

	// Open source
	f, err := os.Open("./src")
	if err != nil {
		log.Fatalln(err)
	}

	defer f.Close()

	capBytes := int64(capacity.BlockSize * capacity.MaxLBA)
	log.Printf("capacity of %d (%d blocks at %d block size)\n", capBytes, capacity.MaxLBA, capacity.BlockSize)

	// Split the capacity into N sections that we'll write to
	sectionSize := int64(capBytes / int64(n))
	log.Printf("running %d sections of size %d MiB with %d workers\n", n, sectionSize/MiB, maxConcurrency)
	semaphore := make(chan bool, maxConcurrency)

	wg := sync.WaitGroup{}

	currentSection := int64(0) // it seemed to stall out at 100 so I started it back up (somehow the iops got lowered)
	for currentSection < n {
		wg.Add(1)
		semaphore <- true
		go func(section int64) {
			device := iscsi.New(iscsi.ConnectionDetails{
				InitiatorIQN: os.Args[1],
				TargetURL:    os.Args[2],
			})
			defer func() {
				<-semaphore
				_ = device.Disconnect()
			}()

			err = device.Connect()
			if err != nil {
				log.Fatalln(err)
			}

			bytesWritten := int64(0)
			currentBlock := int((section * sectionSize) / int64(capacity.BlockSize)) // TODO: Must be coordinated from the top

			log.Println("Connected, starting at", currentBlock)

			for bytesWritten < sectionSize {
				data := make([]byte, writeBlocks*capacity.BlockSize)

				bytesWritten += int64(len(data))

				//// TODO: Configurable source
				//_, err := f.ReadAt(data, 0)
				//if err != nil {
				//	panic(err)
				//}

				_, err := rand.Read(data)
				if err != nil {
					panic(err)
				}
				err = device.Write16(iscsi.Write16{
					LBA:       currentBlock,
					Data:      data,
					BlockSize: capacity.BlockSize,
				})
				if err != nil {
					log.Fatalln(err)
				}
				if bytesWritten%(MiB*100) == 0 {
					log.Printf("Section %d 'wrote' %dMib at %d\n", section, bytesWritten/(100*MiB)*100, currentBlock)
				}
				currentBlock = currentBlock + writeBlocks
			}
			log.Printf("Done with %d %d\n", currentSection, bytesWritten)
		}(currentSection)
		currentSection += 1
	}

	wg.Wait()
}
