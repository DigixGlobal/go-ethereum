package ethdb

import (
	"time"

	"github.com/ethereum/go-ethereum/compression/rle"
	"github.com/ethereum/go-ethereum/logger"
	"github.com/ethereum/go-ethereum/logger/glog"
	"github.com/rcrowley/go-metrics"
	"github.com/syndtr/goleveldb/leveldb"
	"github.com/syndtr/goleveldb/leveldb/errors"
	"github.com/syndtr/goleveldb/leveldb/iterator"
	"github.com/syndtr/goleveldb/leveldb/opt"
)

var OpenFileLimit = 64

type LDBDatabase struct {
	fn string      // filename for reporting
	db *leveldb.DB // LevelDB instance

	GetTimer   metrics.Timer // Timer for measuring the database get request counts and latencies
	PutTimer   metrics.Timer // Timer for measuring the database put request counts and latencies
	DelTimer   metrics.Timer // Timer for measuring the database delete request counts and latencies
	MissMeter  metrics.Meter // MEter for measuring the missed database get requests
	ReadMeter  metrics.Meter // Meter for measuring the database get request data usage
	WriteMeter metrics.Meter // Meter for measuring the database put request data usage
}

// NewLDBDatabase returns a LevelDB wrapped object. LDBDatabase does not persist data by
// it self but requires a background poller which syncs every X. `Flush` should be called
// when data needs to be stored and written to disk.
func NewLDBDatabase(file string) (*LDBDatabase, error) {
	// Open the db
	db, err := leveldb.OpenFile(file, &opt.Options{OpenFilesCacheCapacity: OpenFileLimit})
	// check for curruption and attempt to recover
	if _, iscorrupted := err.(*errors.ErrCorrupted); iscorrupted {
		db, err = leveldb.RecoverFile(file, nil)
	}
	// (re) check for errors and abort if opening of the db failed
	if err != nil {
		return nil, err
	}
	database := &LDBDatabase{
		fn: file,
		db: db,
	}

	return database, nil
}

// Put puts the given key / value to the queue
func (self *LDBDatabase) Put(key []byte, value []byte) error {
	// Measure the database put latency, if requested
	if self.PutTimer != nil {
		start := time.Now()
		defer self.PutTimer.UpdateSince(start)
	}
	// Generate the data to write to disk, update the meter and write
	dat := rle.Compress(value)

	if self.WriteMeter != nil {
		self.WriteMeter.Mark(int64(len(dat)))
	}
	return self.db.Put(key, dat, nil)
}

// Get returns the given key if it's present.
func (self *LDBDatabase) Get(key []byte) ([]byte, error) {
	// Measure the database get latency, if requested
	if self.GetTimer != nil {
		start := time.Now()
		defer self.GetTimer.UpdateSince(start)
	}
	// Retrieve the key and increment the miss counter if not found
	dat, err := self.db.Get(key, nil)
	if err != nil {
		if self.MissMeter != nil {
			self.MissMeter.Mark(1)
		}
		return nil, err
	}
	// Otherwise update the actually retrieved amount of data
	if self.ReadMeter != nil {
		self.ReadMeter.Mark(int64(len(dat)))
	}
	return rle.Decompress(dat)
}

// Delete deletes the key from the queue and database
func (self *LDBDatabase) Delete(key []byte) error {
	// Measure the database delete latency, if requested
	if self.DelTimer != nil {
		start := time.Now()
		defer self.DelTimer.UpdateSince(start)
	}
	// Execute the actual operation
	return self.db.Delete(key, nil)
}

func (self *LDBDatabase) NewIterator() iterator.Iterator {
	return self.db.NewIterator(nil, nil)
}

// Flush flushes out the queue to leveldb
func (self *LDBDatabase) Flush() error {
	return nil
}

func (self *LDBDatabase) Close() {
	if err := self.Flush(); err != nil {
		glog.V(logger.Error).Infof("error: flush '%s': %v\n", self.fn, err)
	}

	self.db.Close()
	glog.V(logger.Error).Infoln("flushed and closed db:", self.fn)
}
