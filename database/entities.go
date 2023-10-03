package database

import (
	"time"
)

// BaseEntity is an abstract entity, all other entities should be derived from it
type BaseEntity struct {
	ID uint64 `gorm:"primaryKey"`
}

type State struct {
	BaseEntity
	Name           string `gorm:"type:varchar(50);index"`
	NextDBIndex    uint64
	LastChainIndex uint64
	Updated        time.Time
}

type FtsoTransaction struct {
	BaseEntity
	Epoch     uint64
	FuncCall  string `gorm:"type:varchar(50)"`
	Data      string `gorm:"type:varchar(10000)"`
	BlockId   uint64
	Status    uint64
	From      string `gorm:"type:varchar(50)"`
	To        string `gorm:"type:varchar(50)"`
	Timestamp uint64
}

// todo: define exact sizes
type Commit struct {
	BaseEntity
	Epoch      uint64
	Address    string `gorm:"type:varchar(50)"`
	CommitHash string `gorm:"type:varchar(128)"`
}

type Reveal struct {
	BaseEntity
	Epoch      uint64
	Address    string `gorm:"type:varchar(50)"`
	Random     string `gorm:"type:varchar(128)"`
	MerkleRoot string `gorm:"type:varchar(128)"`
	BitVote    string `gorm:"type:varchar(50)"`
	Prices     string `gorm:"type:varchar(1000)"`
}

type SignatureData struct {
	BaseEntity
	Epoch          uint64
	Address        string `gorm:"type:varchar(50)"`
	SignatureEpoch uint64
	MerkleRoot     string `gorm:"type:varchar(128)"`
	Signature      string `gorm:"type:varchar(1000)"`
	Timestamp      uint64
}

type Finalization struct {
	BaseEntity
	Epoch          uint64
	Address        string `gorm:"type:varchar(50)"`
	SignatureEpoch uint64
	MerkleRoot     string `gorm:"type:varchar(128)"`
	Signatures     string `gorm:"type:varchar(10000)"`
	Timestamp      uint64
}

type RewardOffer struct {
	BaseEntity
	Epoch               uint64
	Address             string `gorm:"type:varchar(50)"`
	Amount              uint64
	CurrencyAddress     string `gorm:"type:varchar(50)"`
	OfferSymbol         string `gorm:"type:varchar(8)"`
	QuoteSymbol         string `gorm:"type:varchar(8)"`
	LeadProviders       string `gorm:"type:varchar(1000)"`
	RewardBeltPPM       uint64
	ElasticBandWidthPPM uint64
	IqrSharePPM         uint64
	PctSharePPM         uint64
	RemainderClaimer    string `gorm:"type:varchar(50)"`
}

func (s *State) Update(nextIndex, lastIndex int) {
	s.NextDBIndex = uint64(nextIndex)
	s.LastChainIndex = uint64(lastIndex)
	s.Updated = time.Now()
}

func (s *State) UpdateLastIndex(lastIndex int) {
	s.LastChainIndex = uint64(lastIndex)
	s.Updated = time.Now()
}

func (s *State) UpdateTime() {
	s.Updated = time.Now()
}
