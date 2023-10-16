package database

import (
	"flare-ftso-indexer/indexer/abi"
	"reflect"
	"time"
)

var (
	MethodToInterface = map[string]interface{}{
		abi.FtsoCommit:    Commit{},
		abi.FtsoReveal:    Reveal{},
		abi.FtsoSignature: SignatureData{},
		abi.FtsoFinalize:  Finalization{},
		abi.FtsoOffers:    RewardOffer{},
	}
	InterfaceTypeToMethod = map[string]string{
		reflect.TypeOf(Commit{}).String():        abi.FtsoCommit,
		reflect.TypeOf(Reveal{}).String():        abi.FtsoReveal,
		reflect.TypeOf(SignatureData{}).String(): abi.FtsoSignature,
		reflect.TypeOf(Finalization{}).String():  abi.FtsoFinalize,
		reflect.TypeOf(RewardOffer{}).String():   abi.FtsoOffers,
	}
)

// BaseEntity is an abstract entity, all other entities should be derived from it
type BaseEntity struct {
	ID uint64 `gorm:"primaryKey"`
}

type State struct {
	BaseEntity
	Name           string `gorm:"type:varchar(50);index"`
	NextDBIndex    uint64
	FirstDBIndex   uint64
	LastChainIndex uint64
	Updated        time.Time
}

type FtsoTransaction struct {
	BaseEntity
	Hash      string `gorm:"type:varchar(66)"`
	Epoch     uint64
	FuncCall  string `gorm:"type:varchar(50)"`
	Data      string `gorm:"type:varchar(10000)"`
	BlockId   uint64
	Status    uint64
	From      string `gorm:"type:varchar(42)"`
	To        string `gorm:"type:varchar(42)"`
	Timestamp uint64
}

// todo: define exact sizes
type Commit struct {
	BaseEntity
	Epoch      uint64
	Address    string `gorm:"type:varchar(42)"`
	CommitHash string `gorm:"type:varchar(64)"`
	Timestamp  uint64
	TxHash     string `gorm:"type:varchar(66)"`
}

type Reveal struct {
	BaseEntity
	Epoch      uint64
	Address    string `gorm:"type:varchar(42)"`
	Random     string `gorm:"type:varchar(64)"`
	MerkleRoot string `gorm:"type:varchar(64)"`
	BitVote    string `gorm:"type:varchar(2)"`
	Prices     string `gorm:"type:varchar(1000)"`
	Timestamp  uint64
	TxHash     string `gorm:"type:varchar(66)"`
}

type SignatureData struct {
	BaseEntity
	Epoch          uint64
	SignatureEpoch uint64
	Address        string `gorm:"type:varchar(42)"`
	MerkleRoot     string `gorm:"type:varchar(64)"`
	Signature      string `gorm:"type:varchar(1000)"`
	Timestamp      uint64
	TxHash         string `gorm:"type:varchar(66)"`
}

type Finalization struct {
	BaseEntity
	Epoch          uint64
	SignatureEpoch uint64
	Address        string `gorm:"type:varchar(42)"`
	MerkleRoot     string `gorm:"type:varchar(64)"`
	Signatures     string `gorm:"type:varchar(10000)"`
	Timestamp      uint64
	TxHash         string `gorm:"type:varchar(66)"`
}

type RewardOffer struct {
	BaseEntity
	Epoch               uint64
	Address             string `gorm:"type:varchar(42)"`
	Amount              uint64
	CurrencyAddress     string `gorm:"type:varchar(42)"`
	OfferSymbol         string `gorm:"type:varchar(8)"`
	QuoteSymbol         string `gorm:"type:varchar(8)"`
	LeadProviders       string `gorm:"type:varchar(1000)"`
	RewardBeltPPM       uint64
	ElasticBandWidthPPM uint64
	IqrSharePPM         uint64
	PctSharePPM         uint64
	RemainderClaimer    string `gorm:"type:varchar(42)"`
	Timestamp           uint64
	TxHash              string `gorm:"type:varchar(66)"`
}

func (state *State) UpdateAtStart(startIndex, lastChainIndex int) {
	// if a break among saved blocks in the dataset is created, then we change the guaranties about the starting block
	if int(state.NextDBIndex) < startIndex {
		state.FirstDBIndex = uint64(startIndex)
	}
	state.NextDBIndex = uint64(startIndex)
	state.LastChainIndex = uint64(lastChainIndex)
	state.Updated = time.Now()
}

func (s *State) UpdateNextIndex(nextIndex int) {
	s.NextDBIndex = uint64(nextIndex)
	s.Updated = time.Now()
}

func (s *State) UpdateLastIndex(lastIndex int) {
	s.LastChainIndex = uint64(lastIndex)
	s.Updated = time.Now()
}

func (s *State) UpdateTime() {
	s.Updated = time.Now()
}
