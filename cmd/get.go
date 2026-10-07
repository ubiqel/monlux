package main

import (
	"context"
	"fmt"

	"github.com/ubiqel/shine/internal/i2c"
	"github.com/ubiqel/shine/internal/lock"
	"github.com/urfave/cli/v3"
)

func runGetCmd(ctx context.Context, cmd *cli.Command) error {
	lck, err := lock.Acquire()
	if err != nil {
		return fmt.Errorf("acquiring lock: %w\n", err)
	}
	defer lock.Release(lck)

	bus, err := resolveDisplayBus(cmd.StringArg("display"))
	if err != nil {
		return err
	}

	fd, err := openBus(bus)
	if err != nil {
		return fmt.Errorf("opening fd for bus '%v': %w", bus, err)
	}
	pct, _, _, err := i2c.ReadBrightness(fd)
	if err != nil {
		return fmt.Errorf("reading brightness from i2c bus '%v': %w", bus, err)
	}

	fmt.Printf("%d\n", pct)

	return nil
}
