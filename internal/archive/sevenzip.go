package archive

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/tonbiattack/archive-extract-cleaner/internal/model"
	"github.com/tonbiattack/archive-extract-cleaner/internal/winpath"
)

// 7-Zip CLI を使う実装。
//
// 7z.dll は LGPL（一部 unRAR 制限）で、外部プロセスとして呼ぶ限り
// 本アプリの配布物へ取り込む必要はない。ユーザー環境の 7z.exe を使う。
// 未インストールの場合は ErrToolMissing を返し、呼び出し側が UNVERIFIED にする。

const defaultToolName = "7z.exe"

// sevenZipArchive は 7z / RAR を 7-Zip CLI 経由で読む実装。
type sevenZipArchive struct {
	path     string
	size     int64
	modTime  time.Time
	tool     string
	entries  []Entry
	variants []Variant
	byPath   map[string]Entry
	total    int64
}

func openSevenZip(path string, info os.FileInfo, limits model.Limits) (Archive, error) {
	tool, ok := findSevenZip()
	if !ok {
		return nil, ErrToolMissing
	}
	entries, total, err := listSevenZip(tool, path, limits)
	if err != nil {
		return nil, err
	}
	a := &sevenZipArchive{
		path:    path,
		size:    info.Size(),
		modTime: info.ModTime(),
		tool:    tool,
		entries: entries,
		byPath:  make(map[string]Entry, len(entries)),
		total:   total,
	}
	files := map[string]Entry{}
	dirs := map[string]Entry{}
	for _, entry := range entries {
		a.byPath[entry.Key] = entry
		if entry.Dir {
			dirs[entry.Key] = entry
		} else {
			files[entry.Key] = entry
		}
	}
	if err := checkConflicts(files, dirs); err != nil {
		return nil, err
	}
	a.variants = buildVariants(files, dirs)
	return a, nil
}

func (a *sevenZipArchive) Path() string        { return a.path }
func (a *sevenZipArchive) Size() int64         { return a.size }
func (a *sevenZipArchive) ModTime() time.Time  { return a.modTime }
func (a *sevenZipArchive) EntryCount() int     { return len(a.entries) }
func (a *sevenZipArchive) Variants() []Variant { return a.variants }
func (a *sevenZipArchive) StructureHash() string {
	return structureHashOf(a.entries)
}

func (a *sevenZipArchive) Close() error { return nil }

// Open は 7z x -so で1エントリをストリーム展開する。
//
// 7-Zip の -so は、複数エントリを指定してもアーカイブ内の格納順で
// 連結出力する（指定順ではない）。そのためバッチ読み出しは使えず、
// 1エントリ1プロセスになる。実測で約13ms/エントリかかるため、
// エントリ数の多い 7z / RAR は ZIP より遅くなる。
// 速度は ZIP ハッシュキャッシュ（internal/cache）で補う。
func (a *sevenZipArchive) Open(entry Entry) (io.ReadCloser, error) {
	ctx := context.Background()
	cmd := exec.CommandContext(ctx, a.tool, "x", "-so", "-bd", a.path, entry.Path)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	stderr := &bytes.Buffer{}
	cmd.Stderr = stderr
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return &commandReader{cmd: cmd, stdout: stdout, stderr: stderr}, nil
}

// commandReader は子プロセスの標準出力を ReadCloser として扱う。
type commandReader struct {
	cmd    *exec.Cmd
	stdout io.ReadCloser
	stderr *bytes.Buffer
	once   sync.Once
	err    error
}

func (r *commandReader) Read(p []byte) (int, error) {
	n, err := r.stdout.Read(p)
	if err != nil && err != io.EOF {
		r.remember(err)
	}
	return n, err
}

func (r *commandReader) Close() error {
	r.once.Do(func() {
		// 読み切らずに閉じる場合はプロセスを止める。
		if r.cmd.Process != nil {
			_ = r.cmd.Process.Kill()
		}
		waitErr := r.cmd.Wait()
		if waitErr != nil && r.err == nil && !errors.Is(waitErr, os.ErrProcessDone) {
			message := strings.TrimSpace(r.stderr.String())
			if message != "" {
				r.err = fmt.Errorf("%w: %s", ErrInvalid, message)
			} else {
				r.err = waitErr
			}
		}
	})
	return r.err
}

func (r *commandReader) remember(err error) {
	r.once.Do(func() {
		if r.cmd.Process != nil {
			_ = r.cmd.Process.Kill()
		}
		r.err = err
		_ = r.cmd.Wait()
	})
}

// listSevenZip は 7z l -slt の出力を解析してエントリ一覧を作る。
func listSevenZip(tool, path string, limits model.Limits) ([]Entry, int64, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, tool, "l", "-slt", "-bd", path)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	output, err := cmd.Output()
	if err != nil {
		return nil, 0, classifyToolError(err, stderr.String())
	}
	return parseSevenZipListing(output, limits)
}

// parseSevenZipListing は -slt 形式を解析する。
// エントリは空行区切りで、"Key = Value" の並びが続く。
func parseSevenZipListing(data []byte, limits model.Limits) ([]Entry, int64, error) {
	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)

	seen := map[string]struct{}{}
	var entries []Entry
	var total int64
	var current map[string]string
	started := false

	flush := func() error {
		if current == nil {
			return nil
		}
		entry, ok, err := entryFromFields(current)
		if err != nil {
			return err
		}
		if ok {
			if _, duplicate := seen[entry.Key]; duplicate {
				return fmt.Errorf("%w: duplicate path %q", ErrInvalid, entry.Path)
			}
			seen[entry.Key] = struct{}{}
			entries = append(entries, entry)
			total += entry.Size
			if len(entries) > limits.MaxEntries {
				return ErrLimit
			}
			if entry.Size > 0 && uint64(entry.Size) > limits.MaxFileBytes {
				return ErrLimit
			}
			if total > 0 && uint64(total) > limits.MaxTotalBytes {
				return ErrLimit
			}
		}
		current = nil
		return nil
	}

	for scanner.Scan() {
		line := scanner.Text()
		if !started {
			// ヘッダ部の終わりは "----------" 行。
			if strings.HasPrefix(strings.TrimSpace(line), "----------") {
				started = true
			}
			continue
		}
		if strings.TrimSpace(line) == "" {
			if err := flush(); err != nil {
				return nil, 0, err
			}
			continue
		}
		index := strings.Index(line, " = ")
		if index < 0 {
			continue
		}
		key := strings.TrimSpace(line[:index])
		value := line[index+3:]
		if key == "Path" && current != nil {
			// 空行が無い形式に備え、Path が来たら前のエントリを確定する。
			if _, exists := current["Path"]; exists {
				if err := flush(); err != nil {
					return nil, 0, err
				}
			}
		}
		if current == nil {
			current = map[string]string{}
		}
		current[key] = strings.TrimSpace(value)
	}
	if err := scanner.Err(); err != nil {
		return nil, 0, err
	}
	if err := flush(); err != nil {
		return nil, 0, err
	}
	if !started {
		return nil, 0, fmt.Errorf("%w: unexpected 7z output", ErrInvalid)
	}
	return entries, total, nil
}

// entryFromFields は1エントリ分のフィールドから Entry を作る。
func entryFromFields(fields map[string]string) (Entry, bool, error) {
	name, ok := fields["Path"]
	if !ok || name == "" {
		return Entry{}, false, nil
	}
	if encrypted := fields["Encrypted"]; encrypted != "" && !strings.EqualFold(encrypted, "-") {
		return Entry{}, false, fmt.Errorf("%w: %s", ErrEncrypted, name)
	}
	attributes := fields["Attributes"]
	// 7-Zip はディレクトリに "D" を含む属性を付ける（Windows では "D...."）。
	isDir := strings.Contains(attributes, "D")
	if !isDir && strings.HasSuffix(name, "/") {
		isDir = true
	}

	// 7-Zip は Windows では "\" 区切りで返す。
	normalized := strings.ReplaceAll(name, "\\", "/")
	key, display, dirFromPath, err := winpath.NormalizeArchiveEntry(normalized)
	if err != nil {
		return Entry{}, false, fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	if isDir != dirFromPath {
		isDir = dirFromPath || isDir
	}

	entry := Entry{
		Key:  key,
		Path: display,
		Dir:  isDir,
	}
	if !isDir {
		if size, err := strconv.ParseInt(fields["Size"], 10, 64); err == nil && size >= 0 {
			entry.Size = size
		}
		if crc, err := strconv.ParseUint(fields["CRC"], 16, 32); err == nil {
			entry.CRC32 = uint32(crc)
		}
	}
	if modified := fields["Modified"]; modified != "" {
		if parsed, err := parseSevenZipTime(modified); err == nil {
			entry.ModTime = parsed
		}
	}
	return entry, true, nil
}

// parseSevenZipTime は "2026-11-15 16:46:50.4544794" 形式を解析する。
func parseSevenZipTime(value string) (time.Time, error) {
	layouts := []string{
		"2006-01-02 15:04:05.9999999",
		"2006-01-02 15:04:05",
	}
	for _, layout := range layouts {
		if parsed, err := time.ParseInLocation(layout, value, time.Local); err == nil {
			return parsed, nil
		}
	}
	return time.Time{}, fmt.Errorf("unrecognized time %q", value)
}

// classifyToolError は 7-Zip の終了コードとメッセージから原因を切り分ける。
func classifyToolError(err error, stderr string) error {
	message := strings.ToLower(stderr)
	switch {
	case strings.Contains(message, "wrong password"), strings.Contains(message, "encrypted"):
		return fmt.Errorf("%w: %s", ErrEncrypted, strings.TrimSpace(stderr))
	case strings.Contains(message, "can not open"), strings.Contains(message, "cannot open"):
		return fmt.Errorf("%w: %s", ErrUnsupported, strings.TrimSpace(stderr))
	case strings.Contains(message, "is not supported"):
		return fmt.Errorf("%w: %s", ErrUnsupported, strings.TrimSpace(stderr))
	default:
		return err
	}
}

// checkConflicts はファイルとディレクトリのパス衝突を拒否する。
func checkConflicts(files, dirs map[string]Entry) error {
	for key := range files {
		if _, ok := dirs[key]; ok {
			return fmt.Errorf("%w: path is both file and directory", ErrInvalid)
		}
	}
	return nil
}
