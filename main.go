package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const defaultGeoSiteURL = "https://github.com/Loyalsoldier/v2ray-rules-dat/raw/refs/heads/release/geosite.dat"

type options struct {
	geoSitePath string
	outputDir   string
	codes       string
	downloadURL string
}

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run(args []string, stdout, stderr io.Writer) error {
	var opts options
	fs := flag.NewFlagSet("geosite-surge", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.StringVar(&opts.geoSitePath, "geosite", "geosite.dat", "path to geosite.dat; downloaded when missing")
	fs.StringVar(&opts.outputDir, "out", "surge-rules", "directory for generated Surge .list files")
	fs.StringVar(&opts.codes, "codes", "", "comma-separated geosite codes to export; empty means all")
	fs.StringVar(&opts.downloadURL, "url", defaultGeoSiteURL, "geosite.dat download URL")
	if err := fs.Parse(args); err != nil {
		return err
	}

	if err := ensureGeoSiteFile(opts.geoSitePath, opts.downloadURL); err != nil {
		return err
	}

	file, err := os.Open(opts.geoSitePath)
	if err != nil {
		return err
	}
	defer file.Close()

	list, err := readGeoSiteList(file)
	if err != nil {
		return err
	}
	idx := newGeoIndex(list)

	codes := parseCodes(opts.codes)
	if len(codes) == 0 {
		codes = idx.codes()
	}
	if len(codes) == 0 {
		return errors.New("no geosite codes found")
	}

	files, err := buildRuleFiles(idx, codes)
	if err != nil {
		return err
	}
	if err := writeRuleFiles(opts.outputDir, files); err != nil {
		return err
	}

	fmt.Fprintf(stdout, "Generated %d Surge rule files in %s\n", len(files), opts.outputDir)
	return nil
}

func ensureGeoSiteFile(path, url string) error {
	if _, err := os.Stat(path); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("download geosite.dat: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download geosite.dat: unexpected status %s", resp.Status)
	}

	tmp := path + ".tmp"
	out, err := os.Create(tmp)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(out, resp.Body)
	closeErr := out.Close()
	if copyErr != nil {
		_ = os.Remove(tmp)
		return copyErr
	}
	if closeErr != nil {
		_ = os.Remove(tmp)
		return closeErr
	}
	return os.Rename(tmp, path)
}

func parseCodes(value string) []string {
	var out []string
	for _, item := range strings.Split(value, ",") {
		item = strings.TrimSpace(item)
		if item != "" {
			out = append(out, item)
		}
	}
	return out
}

func writeRuleFiles(outDir string, files []ruleFile) error {
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return err
	}
	for _, file := range files {
		path := ruleFilePath(outDir, file)
		out, err := os.Create(path)
		if err != nil {
			return err
		}
		for _, rule := range file.Rules {
			if _, err := fmt.Fprintln(out, rule); err != nil {
				_ = out.Close()
				return err
			}
		}
		if err := out.Close(); err != nil {
			return err
		}
	}
	return nil
}
