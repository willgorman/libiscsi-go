package main

import (
	"errors"
	"fmt"
	"log"
	"os"
	"sync"
	"time"

	"github.com/ceph/go-ceph/rados"
	"github.com/ceph/go-ceph/rbd"
	"github.com/cheggaaa/pb"
	_ "github.com/ianlancetaylor/cgosymbolizer"
	iscsi "github.com/willgorman/libiscsi-go"
)

type reader struct {
	dev       dev
	blockSize int
	blocks    int
	offset    int
}

type dev interface {
	Read16(data iscsi.Read16) ([]byte, error)
	Disconnect() error
	GetFD() int
}

func New(initiatorIQN, targetURL string, numReaders int) ([]*reader, error) {
	d := iscsi.New(iscsi.ConnectionDetails{
		InitiatorIQN: initiatorIQN,
		TargetURL:    targetURL,
	})
	if err := d.Connect(); err != nil {
		return nil, err
	}
	cap, err := d.ReadCapacity10()
	if err != nil {
		return nil, err
	}
	if numReaders == 1 {
		return []*reader{{
			dev:       d,
			blockSize: cap.BlockSize,
			blocks:    cap.LBA + 1, // FIXME: (willgorman) figure out why this looks to be 1 off
		}}, nil
	}
	var readers []*reader

	// FIXME: (willgorman) what if this isn't a multiple of readers
	// panic for now
	if (cap.LBA+1)%numReaders != 0 {
		panic("not supported yet")
	}
	blocksPerReader := (cap.LBA + 1) / numReaders
	var offset int
	for i := 0; i < numReaders; i++ {
		if i == 0 {
			// reuse the session for the first
			readers = append(readers, &reader{
				dev:       d,
				blockSize: cap.BlockSize,
				blocks:    blocksPerReader,
				offset:    offset,
			})
		} else {
			newD := iscsi.New(iscsi.ConnectionDetails{
				InitiatorIQN: initiatorIQN,
				TargetURL:    targetURL,
			})
			if err := newD.Connect(); err != nil {
				return nil, err
			}
			readers = append(readers, &reader{
				dev:       newD,
				blockSize: cap.BlockSize,
				blocks:    blocksPerReader,
				offset:    offset,
			})
		}
		offset += blocksPerReader
	}
	return readers, nil
}

func (r *reader) Close() error {
	return r.dev.Disconnect()
}

func (r *reader) Read(p []byte) (n int, err error) {
	panic("not implemented") // TODO: Implement
}

func (r *reader) ReadAt(p []byte, off int64) (n int, err error) {
	start := (off / int64(r.blockSize)) + int64(r.offset)
	// if start > int64(r.blocks) {
	// 	// TODO: (willgorman) or if len(p)+start is > blocks*blocksize
	// 	return 0, errors.New("out of bounds")
	// }
	if off%int64(r.blockSize) != 0 {
		log.Fatal("only supporting block aligned reads for now ", off)
	}

	// TODO: (willgorman) deal with non-block aligned reads.  might need to read into the next block
	blocks := (len(p) / r.blockSize)
	if (int(start)+blocks)*r.blockSize > (r.blockSize*r.blocks)+r.offset*r.blockSize {
		return 0, errors.New("out of bounds")
	}
	attempts := 0
read16:
	readbytes, err := r.dev.Read16(iscsi.Read16{
		LBA:       int(start),
		Blocks:    blocks,
		BlockSize: r.blockSize,
	})
	attempts++
	if err != nil {
		if err.Error() == "Poll failed" && attempts < 10 {
			goto read16
		}
		return 0, err // TODO: return the bytes read anyway?
	}
	// FIXME: (willgorman) handle non block aligned reads and reads of partial blocks
	// startIndex := off % int64(r.blockSize)
	// endIndex := r.blockSize - int(startIndex)
	// p = readbytes[startIndex:endIndex]

	return copy(p, readbytes), nil
}

func main() {
	if len(os.Args) != 7 {
		panic("missing required args")
	}
	initiatorIQN := os.Args[1]
	targetURL := os.Args[2]
	poolName := os.Args[3]
	imageName := os.Args[4]
	monitors := os.Args[5]
	key := os.Args[6]

	conn, err := cephConnFromOptions(monitors, key)
	if err != nil {
		log.Fatal(err)
	}

	ioctx, err := conn.OpenIOContext(poolName)
	if err != nil {
		log.Fatal(err)
	}
	defer ioctx.Destroy()

	img, err := rbd.OpenImage(ioctx, imageName, rbd.NoSnapshot)
	if err != nil {
		log.Fatal(err)
	}
	defer img.Close()

	readers, err := New(initiatorIQN, targetURL, 4)
	if err != nil {
		log.Fatal(err)
	}
	start := time.Now()
	blockChunk := 2048
	var totalBlocks int
	for _, reader := range readers {
		totalBlocks += reader.blocks
	}
	bar := pb.StartNew(totalBlocks / blockChunk)
	var waiter sync.WaitGroup
	log.Printf("using %d readers ", len(readers))
	waiter.Add(len(readers))
	first := 0
	for _, r := range readers {
		first++
		go func(reader *reader, n int) {
			for i := 0; i < (reader.blockSize * reader.blocks); i = i + (reader.blockSize * blockChunk) {

				thebytes := make([]byte, reader.blockSize*blockChunk)
				_, err := reader.ReadAt(thebytes, int64(i))
				if err != nil {
					log.Fatal(err)
				}
				// FIXME: (willgorman) I think we have to check for ranges of empty blocks
				// otherwise we write zeros to the target.  that may be fine but it seems
				// like it makes Ceph count that space as used
				// or we could call Sparsify on the image after? but Sparsify can't go
				// lower than 4096 so we could have up to 4 empty sequential blocks still allocated?
				if _, err = img.WriteAt(thebytes, int64(i+reader.offset*reader.blockSize)); err != nil {
					log.Fatal("writing ", err)
				}
				if err = img.Flush(); err != nil {
					log.Fatal("flush ", err)
				}

				bar.Increment()
			}
			waiter.Done()
		}(r, first)
	}

	waiter.Wait()
	bar.Finish()
	log.Printf("took %s", time.Since(start))

	err = img.Sparsify(4096)
	if err != nil {
		log.Fatal("sparsify ", err)
	}

	// img2, err := rbd.OpenImage(ioctx, imageName, rbd.NoSnapshot)
	// if err != nil {
	// 	log.Fatal(err)
	// }
	// defer img2.Close()

	// for i := 0; i < 10; i++ {
	// 	thebytes := make([]byte, reader.blockSize)
	// 	_, err := img2.ReadAt(thebytes, int64(i*reader.blockSize))
	// 	if err != nil {
	// 		log.Fatal(err)
	// 	}
	// }

	// fmt.Println("got the image boss")
}

func cephConnFromConfig(confPath, keyringPath string) (*rados.Conn, error) {
	conn, err := rados.NewConnWithUser("admin")
	if err != nil {
		return nil, fmt.Errorf("unable to create ceph connection: %w", err)
	}

	if err := conn.ReadConfigFile(confPath); err != nil {
		return nil, fmt.Errorf("error reading ceph config file: %w", err)
	}
	if err := conn.ReadConfigFile(keyringPath); err != nil {
		return nil, fmt.Errorf("error reading ceph keyring file: %w", err)
	}

	if err := conn.Connect(); err != nil {
		return nil, fmt.Errorf("error connecting to ceph: %w", err)
	}
	return conn, nil
}

func cephConnFromOptions(monitors, key string) (*rados.Conn, error) {
	conn, err := rados.NewConnWithUser("admin")
	if err != nil {
		return nil, fmt.Errorf("unable to create ceph connection: %w", err)
	}

	if err := conn.SetConfigOption("mon_host", monitors); err != nil {
		return nil, fmt.Errorf("error setting mon_host: %w", err)
	}
	if err := conn.SetConfigOption("key", key); err != nil {
		return nil, fmt.Errorf("error setting key: %w", err)
	}

	if err := conn.Connect(); err != nil {
		return nil, fmt.Errorf("error connecting to ceph: %w", err)
	}
	return conn, nil
}
