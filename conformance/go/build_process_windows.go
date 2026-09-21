package conformance

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf16"
	"unsafe"

	"golang.org/x/sys/windows"
)

func runOwnedBuild(ctx context.Context, cmd *exec.Cmd, output *os.File) error {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(job)
	var limits windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION
	limits.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	if _, err := windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation, uintptr(unsafe.Pointer(&limits)), uint32(unsafe.Sizeof(limits))); err != nil {
		return err
	}
	attributes, err := windows.NewProcThreadAttributeList(2)
	if err != nil {
		return err
	}
	defer attributes.Delete()
	// Windows 10's JOB_LIST assigns ownership before the initial thread runs.
	// AssignProcessToJobObject after Start would let a fast child escape.
	const jobList = 0x0002000d
	if err := attributes.Update(jobList, unsafe.Pointer(&job), unsafe.Sizeof(job)); err != nil {
		return err
	}
	null, err := os.Open(os.DevNull)
	if err != nil {
		return err
	}
	defer null.Close()
	duplicate := func(file *os.File) (windows.Handle, error) {
		var handle windows.Handle
		current := windows.CurrentProcess()
		err := windows.DuplicateHandle(current, windows.Handle(file.Fd()), current, &handle, 0, true, windows.DUPLICATE_SAME_ACCESS)
		return handle, err
	}
	input, err := duplicate(null)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(input)
	combined, err := duplicate(output)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(combined)
	inherited := []windows.Handle{input, combined}
	if err := attributes.Update(windows.PROC_THREAD_ATTRIBUTE_HANDLE_LIST, unsafe.Pointer(&inherited[0]), uintptr(len(inherited))*unsafe.Sizeof(inherited[0])); err != nil {
		return err
	}
	path := cmd.Path
	if !filepath.IsAbs(path) && cmd.Dir != "" {
		path = filepath.Join(cmd.Dir, path)
	}
	application, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	command, err := windows.UTF16PtrFromString(windows.ComposeCommandLine(cmd.Args))
	if err != nil {
		return err
	}
	var directory *uint16
	if cmd.Dir != "" {
		directory, err = windows.UTF16PtrFromString(cmd.Dir)
		if err != nil {
			return err
		}
	}
	environment := cmd.Environ()
	sort.Slice(environment, func(i, j int) bool { return strings.ToUpper(environment[i]) < strings.ToUpper(environment[j]) })
	for _, value := range environment {
		if strings.ContainsRune(value, 0) {
			return errors.New("build environment contains NUL")
		}
	}
	block := utf16.Encode([]rune(strings.Join(environment, "\x00") + "\x00\x00"))
	startup := windows.StartupInfoEx{
		StartupInfo: windows.StartupInfo{
			Cb: uint32(unsafe.Sizeof(windows.StartupInfoEx{})), Flags: windows.STARTF_USESTDHANDLES,
			StdInput: input, StdOutput: combined, StdErr: combined,
		},
		ProcThreadAttributeList: attributes.List(),
	}
	var process windows.ProcessInformation
	flags := uint32(windows.CREATE_NO_WINDOW | windows.CREATE_UNICODE_ENVIRONMENT | windows.EXTENDED_STARTUPINFO_PRESENT)
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := windows.CreateProcess(application, command, nil, nil, true, flags, &block[0], directory, &startup.StartupInfo, &process); err != nil {
		return err
	}
	defer windows.CloseHandle(process.Process)
	_ = windows.CloseHandle(process.Thread)
	ended, watched := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(watched)
		select {
		case <-ctx.Done():
			_ = windows.TerminateJobObject(job, 1)
		case <-ended:
		}
	}()
	_, waitErr := windows.WaitForSingleObject(process.Process, windows.INFINITE)
	close(ended)
	<-watched
	cleanup := windows.TerminateJobObject(job, 1)
	if waitErr != nil {
		return errors.Join(waitErr, cleanup)
	}
	var status uint32
	if err := windows.GetExitCodeProcess(process.Process, &status); err != nil {
		return errors.Join(err, cleanup)
	}
	if status != 0 {
		return errors.Join(fmt.Errorf("exit status %d", status), cleanup)
	}
	return errors.Join(ctx.Err(), cleanup)
}
