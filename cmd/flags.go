package main

import "github.com/urfave/cli/v3"

const noBrightnessCacheFlag = "no-brightness-cache"

func noBrightnessCacheFlagDef() *cli.BoolFlag {
	return &cli.BoolFlag{
		Name:  noBrightnessCacheFlag,
		Usage: "read and write brightness on the monitor, ignoring the brightness cache",
	}
}
