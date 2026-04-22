// boltdb storage
package store

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"

	"distributed-editor/internal/raft"
	bolt "go.etcd.io/bbolt"
)

var (
	bucketLog    = []byte("raft-log")    // log bucket
	bucketStable = []byte("raft-stable") // stable bucket
)

// boltdb store
type BoltStore struct {
	db *bolt.DB
}

// create store
func NewBoltStore(path string) (*BoltStore, error) {
	db, err := bolt.Open(path, 0600, nil)
	if err != nil {
		return nil, fmt.Errorf("boltstore: open %q: %w", path, err)
	}

	// Create buckets if they don't exist.
	if err := db.Update(func(tx *bolt.Tx) error {
		if _, err := tx.CreateBucketIfNotExists(bucketLog); err != nil {
			return err
		}
		if _, err := tx.CreateBucketIfNotExists(bucketStable); err != nil {
			return err
		}
		return nil
	}); err != nil {
		db.Close()
		return nil, fmt.Errorf("boltstore: init buckets: %w", err)
	}

	return &BoltStore{db: db}, nil
}

// close database
func (s *BoltStore) Close() error {
	return s.db.Close()
}

// ─────────────────────────────────────────────
// raft.LogStore implementation
// ─────────────────────────────────────────────

// get first index
func (s *BoltStore) FirstIndex() (uint64, error) {
	var idx uint64
	err := s.db.View(func(tx *bolt.Tx) error {
		c := tx.Bucket(bucketLog).Cursor()
		k, _ := c.First()
		if k != nil {
			idx = bytesToUint64(k)
		}
		return nil
	})
	return idx, err
}

// get last index
func (s *BoltStore) LastIndex() (uint64, error) {
	var idx uint64
	err := s.db.View(func(tx *bolt.Tx) error {
		c := tx.Bucket(bucketLog).Cursor()
		k, _ := c.Last()
		if k != nil {
			idx = bytesToUint64(k)
		}
		return nil
	})
	return idx, err
}

// get log entry
func (s *BoltStore) GetLog(index uint64) (*raft.LogEntry, error) {
	var out raft.LogEntry
	err := s.db.View(func(tx *bolt.Tx) error {
		v := tx.Bucket(bucketLog).Get(uint64ToBytes(index))
		if v == nil {
			return raft.ErrLogNotFound
		}
		return json.Unmarshal(v, &out)
	})
	return &out, err
}

// save single log
func (s *BoltStore) StoreLog(log *raft.LogEntry) error {
	return s.StoreLogs([]*raft.LogEntry{log})
}

// save log batch
func (s *BoltStore) StoreLogs(logs []*raft.LogEntry) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucketLog)
		for _, l := range logs {
			data, err := json.Marshal(l)
			if err != nil {
				return fmt.Errorf("marshal log %d: %w", l.Index, err)
			}
			if err := b.Put(uint64ToBytes(l.Index), data); err != nil {
				return fmt.Errorf("put log %d: %w", l.Index, err)
			}
		}
		return nil
	})
}

// delete log range
func (s *BoltStore) DeleteRange(min, max uint64) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucketLog)
		c := b.Cursor()
		for k, _ := c.Seek(uint64ToBytes(min)); k != nil; k, _ = c.Next() {
			if bytesToUint64(k) > max {
				break
			}
			if err := c.Delete(); err != nil {
				return err
			}
		}
		return nil
	})
}

// ─────────────────────────────────────────────
// raft.StableStore implementation
// ─────────────────────────────────────────────

// set value
func (s *BoltStore) Set(key []byte, val []byte) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		return tx.Bucket(bucketStable).Put(key, val)
	})
}

// get value
func (s *BoltStore) Get(key []byte) ([]byte, error) {
	var val []byte
	err := s.db.View(func(tx *bolt.Tx) error {
		v := tx.Bucket(bucketStable).Get(key)
		if v != nil {
			val = make([]byte, len(v))
			copy(val, v)
		}
		return nil
	})
	return val, err
}

// set uint64
func (s *BoltStore) SetUint64(key []byte, val uint64) error {
	return s.Set(key, uint64ToBytes(val))
}

// get uint64
func (s *BoltStore) GetUint64(key []byte) (uint64, error) {
	val, err := s.Get(key)
	if err != nil {
		return 0, err
	}
	if len(val) == 0 {
		return 0, nil
	}
	if len(val) != 8 {
		return 0, errors.New("invalid uint64 value in stable store")
	}
	return bytesToUint64(val), nil
}

// ─────────────────────────────────────────────
// Byte encoding helpers
// ─────────────────────────────────────────────

// helper for uint64 to bytes
func uint64ToBytes(v uint64) []byte {
	b := make([]byte, 8)
	binary.BigEndian.PutUint64(b, v)
	return b
}

// helper for bytes to uint64
func bytesToUint64(b []byte) uint64 {
	return binary.BigEndian.Uint64(b)
}
