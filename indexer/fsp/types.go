package fsp

const (
	fspFsmContractName = "FlareSystemsManager"
)

type fspBlockRange struct {
	from uint64
	to   uint64
}

type fspStartupTargets struct {
	fullStartBlock      uint64
	fullStartTimestamp  uint64
	eventStartBlock     uint64
	eventStartTimestamp uint64
	eventRanges         []fspBlockRange
}
