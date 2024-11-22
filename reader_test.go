package iscsi_test

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"io"
	"log"
	"math/rand"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/avast/retry-go/v4"
	iscsi "github.com/willgorman/libiscsi-go"
	"gotest.tools/assert"
)

type testReadDevice struct {
	f *os.File
}

func (t *testReadDevice) ReadCapacity16() (c iscsi.Capacity, err error) {
	panic("not implemented") // TODO: Implement
}

func (t *testReadDevice) Read16(data iscsi.Read16) ([]byte, error) {
	panic("not implemented") // TODO: Implement
}

func (t *testReadDevice) Connect() error {
	return nil
}

func (t *testReadDevice) Disconnect() error {
	return nil
}

func TestRead(t *testing.T) {
	seed := time.Now().UnixNano()
	t.Logf("using seed %d", seed)
	rnd := rand.New(rand.NewSource(seed))
	fileName := writeTargetTempfile(t, rnd, 4*KiB)
	file, err := os.Open(fileName)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		log.Fatal(err)
	}
	fileChecksum := fmt.Sprintf("%x", hash.Sum(nil))
	t.Log("FILE CHECKSUM", fileChecksum)

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

	sreader, err := iscsi.Reader(device)
	if err != nil {
		t.Fatal(err)
	}
	log.Printf("%#v", sreader)
	hash = sha256.New()
	if _, err := io.Copy(hash, sreader); err != nil {
		log.Fatal(err)
	}
	iscsiChecksum := fmt.Sprintf("%x", hash.Sum(nil))
	t.Log("ISCSI CHECKSUM ", iscsiChecksum)
	assert.Equal(t, fileChecksum, iscsiChecksum)
}

// in order to test non block aligned reads
// we can have a file io.Reader and iscsi io.Reader and
// read randomly sized []byte from them and assert that we always get
// the same values from each
func TestReadRandom(t *testing.T) {
	seed := time.Now().UnixNano()
	t.Logf("using seed %d", seed)
	rnd := rand.New(rand.NewSource(seed))
	fileName := writeTargetTempfile(t, rnd, 10*MiB)
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

	sreader, err := iscsi.Reader(device)
	if err != nil {
		t.Fatal(err)
	}

	var fileErr, scsiErr error
	var fileN, scsiN int
	for fileErr != io.EOF && scsiErr != io.EOF {
		n := rnd.Intn(32 * KiB)
		fileBytes := make([]byte, n)
		scsiBytes := make([]byte, n)
		fileN, fileErr = file.Read(fileBytes)
		if fileErr != nil && fileErr != io.EOF {
			t.Fatal(fileErr)
		}
		retry.Do(func() error {
			scsiN, scsiErr = sreader.Read(scsiBytes)
			return scsiErr
		}, retry.RetryIf(func(err error) bool {
			if err != nil && strings.Contains(err.Error(), "Poll failed") {
				return true
			}
			return false
		}), retry.Attempts(0), retry.OnRetry(func(n uint, err error) {
			t.Log("RETRY ", err)
		}))

		if scsiErr != nil && scsiErr != io.EOF {
			// FIXME: (willgorman) something in this path causes a segfault on disconnect
			// immediately after a poll failed
			t.Fatal(scsiErr)
		}
		assert.Equal(t, fileN, scsiN)
		assert.Assert(t, bytes.Equal(fileBytes, scsiBytes))
	}
}

func TestReadLoop(t *testing.T) {
	// seed := time.Now().UnixNano()
	seed := int64(1732045254519287895)
	t.Logf("using seed %d", seed)
	rnd := rand.New(rand.NewSource(seed))
	fileName := writeTargetTempfile(t, rnd, 10*MiB)
	targetURL := runTestTarget(t, fileName)
	device := iscsi.New(iscsi.ConnectionDetails{
		InitiatorIQN: "iqn.2024-10.libiscsi:go",
		TargetURL:    targetURL,
	})

	err := device.Connect()
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = device.Disconnect()
	}()

	for i := 0; i < 1000; i++ {
		t.Log("LOOP ", i)
		sreader, err := iscsi.Reader(device)
		if err != nil {
			t.Fatal(err)
		}
		readAll(t, sreader, rnd)
	}
}

func readAll(t *testing.T, sreader io.Reader, rnd *rand.Rand) {
	var scsiErr error
	for scsiErr != io.EOF {
		n := rnd.Intn(32 * KiB)
		scsiBytes := make([]byte, n)
		retry.Do(func() error {
			_, scsiErr = sreader.Read(scsiBytes)
			return scsiErr
		}, retry.RetryIf(func(err error) bool {
			if err != nil && strings.Contains(err.Error(), "Poll failed") {
				return true
			}
			return false
		}), retry.Attempts(0), retry.OnRetry(func(n uint, err error) {
			t.Log("RETRY ", err)
		}))

		if scsiErr != nil && scsiErr != io.EOF {
			// FIXME: (willgorman) something in this path causes a segfault on disconnect
			// immediately after a poll failed
			t.Fatal(scsiErr)
		}

	}
}
