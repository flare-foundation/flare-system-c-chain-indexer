package fsp

const (
	fspFsmContractName = "FlareSystemsManager"
)

type fspBlockRange struct {
	from uint64
	to   uint64
}

type fspStartupPlan struct {
	fullIndexStartBlock     uint64
	fullIndexStartTimestamp uint64
	keepFromBlock           uint64
	keepFromTimestamp       uint64
	fspEventRanges          []fspBlockRange
}
