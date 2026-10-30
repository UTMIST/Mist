package jobentry

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"golang.org/x/sys/unix"
)

// privateDirs live in $HOME and are reset to 0700 on every job (D8).
var privateDirs = []string{".cache", ".config", ".local", ".ssh"}

// ensurePrivateDirs creates or resets the private dot-folders without
// following symlinks. A replacement that is not a real directory owned by
// the job's user fails setup; its target is never chmod-ed.
func ensurePrivateDirs(home string) error {
	hfd, err := unix.Open(home, unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_RDONLY|unix.O_CLOEXEC, 0)
	if err != nil {
		return fmt.Errorf("open home %s: %w", home, err)
	}
	defer unix.Close(hfd)
	uid := os.Geteuid()
	for _, name := range privateDirs {
		if err := unix.Mkdirat(hfd, name, 0o700); err != nil && !errors.Is(err, unix.EEXIST) {
			return fmt.Errorf("create %s/%s: %w", home, name, err)
		}
		fd, err := unix.Openat(hfd, name, unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_RDONLY|unix.O_CLOEXEC, 0)
		if err != nil {
			return fmt.Errorf("%s/%s is not a real directory (%w); fix or remove it", home, name, err)
		}
		var st unix.Stat_t
		err = unix.Fstat(fd, &st)
		if err == nil && int(st.Uid) != uid {
			err = fmt.Errorf("owned by uid %d, not %d", st.Uid, uid)
		}
		if err == nil {
			err = unix.Fchmod(fd, 0o700)
		}
		unix.Close(fd)
		if err != nil {
			return fmt.Errorf("%s/%s: %w", home, name, err)
		}
	}
	return nil
}

// openOutput opens the output file for appending, resolving the path
// strictly beneath the team root: symlinks may not escape it, and the
// result must be a regular file (never a FIFO or device that could block
// or redirect output).
func openOutput(teamRoot, output string) (*os.File, error) {
	rel := strings.TrimPrefix(strings.TrimPrefix(output, teamRoot), "/")
	if rel == "" || rel == output {
		return nil, fmt.Errorf("output %s is not inside %s", output, teamRoot)
	}
	root, err := unix.Open(teamRoot, unix.O_DIRECTORY|unix.O_RDONLY|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, fmt.Errorf("open team folder: %w", err)
	}
	defer unix.Close(root)
	fd, err := unix.Openat2(root, rel, &unix.OpenHow{
		Flags:   unix.O_WRONLY | unix.O_CREAT | unix.O_APPEND | unix.O_NONBLOCK | unix.O_NOFOLLOW | unix.O_CLOEXEC,
		Mode:    0o666, // umask 0027 makes it 0640
		Resolve: unix.RESOLVE_BENEATH | unix.RESOLVE_NO_MAGICLINKS,
	})
	if err != nil {
		return nil, fmt.Errorf("open output %s: %w", output, err)
	}
	var st unix.Stat_t
	if err := unix.Fstat(fd, &st); err != nil || st.Mode&unix.S_IFMT != unix.S_IFREG {
		unix.Close(fd)
		return nil, fmt.Errorf("output %s is not a regular file", output)
	}
	if err := unix.SetNonblock(fd, false); err != nil {
		unix.Close(fd)
		return nil, err
	}
	return os.NewFile(uintptr(fd), output), nil
}
