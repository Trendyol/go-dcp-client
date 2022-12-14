package godcpclient

import (
	"sync"
	"time"

	"github.com/Trendyol/go-dcp-client/logger"

	"github.com/Trendyol/go-dcp-client/helpers"
	"github.com/couchbase/gocbcore/v10"
)

type Checkpoint interface {
	Save()
	Load() map[uint16]*ObserverState
	Clear()
	StartSchedule()
	StopSchedule()
}

type checkpointDocumentSnapshot struct {
	StartSeqNo uint64 `json:"startSeqno"`
	EndSeqNo   uint64 `json:"endSeqno"`
}

type checkpointDocumentCheckpoint struct {
	VbUUID   uint64                     `json:"vbuuid"`
	SeqNo    uint64                     `json:"seqno"`
	Snapshot checkpointDocumentSnapshot `json:"snapshot"`
}

type CheckpointDocument struct {
	BucketUUID string                       `json:"bucketUuid"`
	Checkpoint checkpointDocumentCheckpoint `json:"checkpoint"`
}

func NewEmptyCheckpointDocument(bucketUUID string) CheckpointDocument {
	return CheckpointDocument{
		Checkpoint: checkpointDocumentCheckpoint{
			VbUUID: 0,
			SeqNo:  0,
			Snapshot: checkpointDocumentSnapshot{
				StartSeqNo: 0,
				EndSeqNo:   0,
			},
		},
		BucketUUID: bucketUUID,
	}
}

type checkpoint struct {
	observer     Observer
	metadata     Metadata
	failoverLogs map[uint16][]gocbcore.FailoverEntry
	vbSeqNos     map[uint16]gocbcore.VbSeqNoEntry
	schedule     *time.Ticker
	bucketUUID   string
	config       helpers.Config
	vbIds        []uint16
	saveLock     sync.Mutex
	loadLock     sync.Mutex
}

func (s *checkpoint) Save() {
	s.saveLock.Lock()
	defer s.saveLock.Unlock()

	state := s.observer.GetState()

	dump := map[uint16]CheckpointDocument{}

	for vbID, observerState := range state {
		dump[vbID] = CheckpointDocument{
			Checkpoint: checkpointDocumentCheckpoint{
				VbUUID: uint64(s.failoverLogs[vbID][0].VbUUID),
				SeqNo:  observerState.SeqNo,
				Snapshot: checkpointDocumentSnapshot{
					StartSeqNo: observerState.StartSeqNo,
					EndSeqNo:   observerState.EndSeqNo,
				},
			},
			BucketUUID: s.bucketUUID,
		}
	}

	s.metadata.Save(dump, s.bucketUUID)

	logger.Debug("saved checkpoint")
}

func (s *checkpoint) Load() map[uint16]*ObserverState {
	s.loadLock.Lock()
	defer s.loadLock.Unlock()

	dump := s.metadata.Load(s.vbIds, s.bucketUUID)

	observerState := map[uint16]*ObserverState{}

	for vbID, doc := range dump {
		observerState[vbID] = &ObserverState{
			SeqNo:      doc.Checkpoint.SeqNo,
			StartSeqNo: doc.Checkpoint.Snapshot.StartSeqNo,
			EndSeqNo:   doc.Checkpoint.Snapshot.EndSeqNo,
		}
	}

	s.observer.SetState(observerState)
	logger.Debug("loaded checkpoint")

	return observerState
}

func (s *checkpoint) Clear() {
	s.metadata.Clear(s.vbIds)
	logger.Debug("cleared checkpoint")
}

func (s *checkpoint) StartSchedule() {
	if s.config.Checkpoint.Type != helpers.CheckpointTypeAuto {
		return
	}

	go func() {
		s.schedule = time.NewTicker(s.config.Checkpoint.Interval)
		for range s.schedule.C {
			s.Save()
		}
	}()
	logger.Debug("started checkpoint schedule")
}

func (s *checkpoint) StopSchedule() {
	if s.config.Checkpoint.Type != helpers.CheckpointTypeAuto {
		return
	}

	if s.schedule != nil {
		s.schedule.Stop()
	}

	logger.Debug("stopped checkpoint schedule")
}

func NewCheckpoint(
	observer Observer,
	vbIds []uint16,
	failoverLogs map[uint16][]gocbcore.FailoverEntry,
	vbSeqNos map[uint16]gocbcore.VbSeqNoEntry,
	bucketUUID string,
	metadata Metadata, config helpers.Config,
) Checkpoint {
	return &checkpoint{
		observer:     observer,
		vbIds:        vbIds,
		failoverLogs: failoverLogs,
		vbSeqNos:     vbSeqNos,
		bucketUUID:   bucketUUID,
		metadata:     metadata,
		config:       config,
	}
}
