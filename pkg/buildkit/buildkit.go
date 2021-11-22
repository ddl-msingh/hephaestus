package buildkit

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/containerd/console"
	"github.com/docker/distribution/reference"
	"github.com/go-logr/logr"
	"github.com/moby/buildkit/client"
	"github.com/moby/buildkit/session"
	"github.com/moby/buildkit/util/progress/progressui"
	"golang.org/x/sync/errgroup"

	"github.com/dominodatalab/hephaestus/pkg/buildkit/archive"
)

type BuildOptions struct {
	Context            string
	Images             []string
	BuildArgs          []string
	DisableCacheExport bool
	DisableCacheImport bool
}

type Client interface {
	Cache(image string) error
	Build(opts BuildOptions) error
}

type ClientOpt func(rc *RemoteClient)

func WithAuthConfig(configDir string) ClientOpt {
	return func(rc *RemoteClient) {
		rc.authConfig = configDir
	}
}

func WithLogger(log logr.Logger) ClientOpt {
	return func(rc *RemoteClient) {
		rc.log = log
	}
}

type RemoteClient struct {
	bk         *client.Client
	ctx        context.Context
	log        logr.Logger
	authConfig string
}

func NewRemoteClient(ctx context.Context, addr string, opts ...ClientOpt) (*RemoteClient, error) {
	bk, err := client.New(ctx, addr, client.WithFailFast()) // TODO: explore adding jaeger tracing option
	if err != nil {
		return nil, fmt.Errorf("failed to create buildkit client: %w", err)
	}

	client := &RemoteClient{
		bk:  bk,
		ctx: ctx,
		log: logr.Discard(),
	}
	for _, opt := range opts {
		opt(client)
	}

	return client, nil
}

func (c *RemoteClient) Cache(image string) error {
	return c.solveWith(func(buildDir string, solveOpt *client.SolveOpt) error {
		dockerfile := filepath.Join(buildDir, "Dockerfile")
		contents := []byte(fmt.Sprintf("FROM %s", image))
		if err := os.WriteFile(dockerfile, contents, 0644); err != nil {
			return fmt.Errorf("failed to create dockerfile: %w", err)
		}

		solveOpt.LocalDirs = map[string]string{
			"context":    buildDir,
			"dockerfile": buildDir,
		}
		solveOpt.Exports = []client.ExportEntry{
			{
				Type: client.ExporterOCI,
				Output: func(m map[string]string) (io.WriteCloser, error) {
					return DiscardCloser{io.Discard}, nil
				},
			},
		}

		return nil
	})
}

func (c *RemoteClient) Build(opts BuildOptions) error {
	return c.solveWith(func(buildDir string, solveOpt *client.SolveOpt) error {
		extract, err := archive.FetchAndExtract(c.log, c.ctx, opts.Context, buildDir, 5*time.Minute)
		if err != nil {
			return err
		}

		solveOpt.LocalDirs = map[string]string{
			"context":    extract.ContentsDir,
			"dockerfile": extract.ContentsDir,
		}

		for _, name := range opts.Images {
			solveOpt.Exports = append(solveOpt.Exports, client.ExportEntry{
				Type: client.ExporterImage,
				Attrs: map[string]string{
					"push": "true",
					"name": name,
				},
			})
		}

		if !opts.DisableCacheExport {
			solveOpt.CacheExports = []client.CacheOptionsEntry{
				{
					Type: "inline",
				},
			}
		}
		if !opts.DisableCacheImport {
			// NOTE: this is presumptive but will always work if pushing a single image
			named, err := reference.ParseNormalizedNamed(opts.Images[0])
			if err != nil {
				return err
			}

			solveOpt.CacheImports = []client.CacheOptionsEntry{
				{
					Type: "registry",
					Attrs: map[string]string{
						"ref": named.Name(),
					},
				},
			}
		}

		return nil
	})
}

func (c *RemoteClient) solveWith(modify func(buildDir string, solveOpt *client.SolveOpt) error) error {
	buildDir, err := os.MkdirTemp("", "hephaestus-build-")
	if err != nil {
		return fmt.Errorf("failed to create build dir: %w", err)
	}
	defer os.RemoveAll(buildDir)

	solveOpt := client.SolveOpt{
		Frontend:      "dockerfile.v0",
		FrontendAttrs: map[string]string{},
		Session: []session.Attachable{
			NewDockerAuthProvider(c.authConfig),
		},
	}

	if err = modify(buildDir, &solveOpt); err != nil {
		return err
	}

	return c.runSolve(solveOpt)
}

func (c *RemoteClient) runSolve(so client.SolveOpt) error {
	lw := &LogWriter{Logger: c.log}

	ch := make(chan *client.SolveStatus)
	eg, ctx := errgroup.WithContext(c.ctx)

	eg.Go(func() error {
		_, err := c.bk.Solve(ctx, nil, so, ch)
		return err
	})
	eg.Go(func() error {
		var c console.Console
		if cn, err := console.ConsoleFromFile(os.Stderr); err != nil {
			c = cn
		}

		return progressui.DisplaySolveStatus(ctx, "", c, lw, ch)
	})

	if err := eg.Wait(); err != nil {
		return fmt.Errorf("buildkit solve issue: %w", err)
	}

	return nil
}
