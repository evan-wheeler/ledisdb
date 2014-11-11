package ledis

import (
	"github.com/siddontang/go/log"
	"github.com/siddontang/ledisdb/rpl"
	"github.com/siddontang/ledisdb/store"
	"sync"
)

type batch struct {
	l *Ledis

	*store.WriteBatch

	sync.Locker

	tx *Tx
}

func (b *batch) Commit() error {
	if b.l.IsReadOnly() {
		return ErrWriteInROnly
	}

	if b.tx == nil {
		return b.l.handleCommit(b.WriteBatch, b.WriteBatch)
	} else {
		if b.l.r != nil {
			if err := b.tx.data.Append(b.WriteBatch.BatchData()); err != nil {
				return err
			}
		}
		return b.WriteBatch.Commit()
	}
}

func (b *batch) Lock() {
	b.Locker.Lock()
}

func (b *batch) Unlock() {
	b.WriteBatch.Rollback()
	b.Locker.Unlock()
}

func (b *batch) Put(key []byte, value []byte) {
	b.WriteBatch.Put(key, value)
}

func (b *batch) Delete(key []byte) {
	b.WriteBatch.Delete(key)
}

type dbBatchLocker struct {
	l      *sync.Mutex
	wrLock *sync.RWMutex
}

func (l *dbBatchLocker) Lock() {
	l.wrLock.RLock()
	l.l.Lock()
}

func (l *dbBatchLocker) Unlock() {
	l.l.Unlock()
	l.wrLock.RUnlock()
}

type txBatchLocker struct {
}

func (l *txBatchLocker) Lock()   {}
func (l *txBatchLocker) Unlock() {}

type multiBatchLocker struct {
}

func (l *multiBatchLocker) Lock()   {}
func (l *multiBatchLocker) Unlock() {}

func (l *Ledis) newBatch(wb *store.WriteBatch, locker sync.Locker, tx *Tx) *batch {
	b := new(batch)
	b.l = l
	b.WriteBatch = wb

	b.Locker = locker

	b.tx = tx

	return b
}

type commiter interface {
	Commit() error
}

type commitDataGetter interface {
	Data() []byte
}

func (l *Ledis) handleCommit(g commitDataGetter, c commiter) error {
	l.commitLock.Lock()
	defer l.commitLock.Unlock()

	var err error
	if l.r != nil {
		var rl *rpl.Log
		if rl, err = l.r.Log(g.Data()); err != nil {
			log.Fatal("write wal error %s", err.Error())
			return err
		}

		l.propagate(rl)

		if err = c.Commit(); err != nil {
			log.Fatal("commit error %s", err.Error())
			l.noticeReplication()
			return err
		}

		if err = l.r.UpdateCommitID(rl.ID); err != nil {
			log.Fatal("update commit id error %s", err.Error())
			l.noticeReplication()
			return err
		}

		return nil
	} else {
		return c.Commit()
	}
}
