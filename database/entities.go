package database

import (
	"flare-ftso-indexer/indexer/abi"
	"reflect"
	"time"

	"gorm.io/gorm"
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
	ID uint64 `gorm:"primaryKey;unique"`
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
	Hash      string `gorm:"type:varchar(64);index;unique"`
	Method    string `gorm:"type:varchar(50);index"`
	Data      string `gorm:"type:varchar(10000)"` // todo: size
	BlockId   uint64
	Status    uint64
	From      string `gorm:"type:varchar(40);index"`
	To        string `gorm:"type:varchar(40);index"`
	Timestamp uint64 `gorm:"index"`
}

type Commit struct {
	BaseEntity
	Epoch      uint64
	Address    string `gorm:"type:varchar(40)"`
	CommitHash string `gorm:"type:varchar(64)"`
	Timestamp  uint64 `gorm:"index"`
	TxHash     string `gorm:"type:varchar(64);unique"`
}

type Reveal struct {
	BaseEntity
	Epoch      uint64
	Address    string `gorm:"type:varchar(40)"`
	Random     string `gorm:"type:varchar(64)"`
	MerkleRoot string `gorm:"type:varchar(64)"`
	BitVote    string `gorm:"type:varchar(2)"`
	Prices     string `gorm:"type:varchar(1000)"`
	Timestamp  uint64 `gorm:"index"`
	TxHash     string `gorm:"type:varchar(64);unique"`
}

type SignatureData struct {
	BaseEntity
	Epoch          uint64
	SignatureEpoch uint64
	Address        string `gorm:"type:varchar(40)"`
	MerkleRoot     string `gorm:"type:varchar(64)"`
	Signature      string `gorm:"type:varchar(1000)"`
	Timestamp      uint64
	TxHash         string `gorm:"type:varchar(64);unique"`
}

type Finalization struct {
	BaseEntity
	Epoch          uint64
	SignatureEpoch uint64
	Address        string `gorm:"type:varchar(40)"`
	MerkleRoot     string `gorm:"type:varchar(64)"`
	Signatures     string `gorm:"type:varchar(10000)"`
	Timestamp      uint64 `gorm:"index"`
	TxHash         string `gorm:"type:varchar(64)"`
}

type RewardOffer struct {
	BaseEntity
	Epoch               uint64
	Address             string `gorm:"type:varchar(40)"`
	Amount              uint64
	CurrencyAddress     string `gorm:"type:varchar(40)"`
	OfferSymbol         string `gorm:"type:varchar(8)"`
	QuoteSymbol         string `gorm:"type:varchar(8)"`
	LeadProviders       string `gorm:"type:varchar(1000)"`
	RewardBeltPPM       uint64
	ElasticBandWidthPPM uint64
	IqrSharePPM         uint64
	PctSharePPM         uint64
	RemainderClaimer    string `gorm:"type:varchar(40)"`
	Timestamp           uint64 `gorm:"index"`
	TxHash              string `gorm:"type:varchar(64)"`
}

func (state *State) UpdateAtStart(startIndex, lastChainIndex int) {
	// if a break among saved blocks in the dataset is created,
	// then we change the guaranties about the starting block
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

func FetchState(db *gorm.DB, name string) (*State, error) {
	var currentState State
	err := db.Where(&State{Name: name}).First(&currentState).Error
	return &currentState, err
}
