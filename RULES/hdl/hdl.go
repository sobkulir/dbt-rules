package hdl

import (
	"dbt-rules/RULES/core"
)

var BoardName = core.StringFlag{
	Name: "board",
	DefaultFn: func() string {
		return "em.avnet.com:ultra96v2:part0:1.0"
	},
}.Register()

var PartName = core.StringFlag{
	Name: "part",
	DefaultFn: func() string {
		return "xczu3eg-sbva484-1-e"
	},
}.Register()

type Ip interface {
	Sources() []core.Path
	Data() []core.Path
	Ips() []Ip
}

type Library struct {
	Srcs      []core.Path
	DataFiles []core.Path
	IpDeps    []Ip
}

func (lib Library) Sources() []core.Path {
	return lib.Srcs
}

func (lib Library) Data() []core.Path {
	return lib.DataFiles
}

func (lib Library) Ips() []Ip {
	return lib.IpDeps
}
