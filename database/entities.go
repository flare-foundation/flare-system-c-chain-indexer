package database

// BaseEntity is an abstract entity, all other entities should be derived from it
type BaseEntity struct {
	ID uint64 `gorm:"primaryKey;unique"`
}

type Transaction struct {
	BaseEntity
	Hash             string `gorm:"type:varchar(64);index;unique"`
	FunctionSig      string `gorm:"type:varchar(50);index"`
	Input            string `gorm:"type:string"`
	BlockNumber      uint64 `gorm:"index"`
	BlockHash        string `gorm:"type:varchar(64)"`
	TransactionIndex uint64
	FromAddress      string `gorm:"type:varchar(40);index"`
	ToAddress        string `gorm:"type:varchar(40);index"`
	Status           uint64
	Value            string `gorm:"type:string"`
	GasPrice         string `gorm:"type:string"`
	Gas              uint64
	Timestamp        uint64 `gorm:"index"`
}

type Log struct {
	BaseEntity
	TransactionID   uint64       `gorm:"default:null"`
	Transaction     *Transaction `gorm:"foreignKey:TransactionID;references:ID;constraint:OnUpdate:CASCADE"`
	Address         string       `gorm:"type:varchar(40);index"`
	Data            string       `gorm:"type:string"`
	Topic0          string       `gorm:"type:varchar(64);index"`
	Topic1          string       `gorm:"type:varchar(64);index"`
	Topic2          string       `gorm:"type:varchar(64);index"`
	Topic3          string       `gorm:"type:varchar(64);index"`
	TransactionHash string       `gorm:"type:varchar(64);uniqueIndex:hash_index_unique"`
	LogIndex        uint64       `gorm:"uniqueIndex:hash_index_unique"`
	Timestamp       uint64       `gorm:"index"`
	BlockNumber     uint64       `gorm:"index"`
}
