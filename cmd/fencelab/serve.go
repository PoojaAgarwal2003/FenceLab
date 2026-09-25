package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"

	"github.com/PoojaAgarwal2003/FenceLab/internal/web"
)

func serve(ctx context.Context, args []string, errOut io.Writer) int {
	flags := flag.NewFlagSet("serve", flag.ContinueOnError)
	flags.SetOutput(errOut)
	address := flags.String("listen", "127.0.0.1:8091", "numeric loopback address and port")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if flags.NArg() != 0 {
		fmt.Fprintln(errOut, "unexpected positional arguments")
		return 2
	}
	if err := web.Serve(ctx, *address, errOut); err != nil {
		fmt.Fprintln(errOut, err)
		return 1
	}
	return 0
}
