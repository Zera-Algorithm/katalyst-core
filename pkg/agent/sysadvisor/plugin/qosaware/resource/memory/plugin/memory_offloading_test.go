package plugin

import (
	"errors"
	"os"
	"testing"
	"time"

	"github.com/bytedance/mockey"
	"github.com/stretchr/testify/assert"

	"github.com/kubewharf/katalyst-core/pkg/agent/qrm-plugins/memory/dynamicpolicy/memoryadvisor"
	"github.com/kubewharf/katalyst-core/pkg/consts"
)

type mockDirEntry struct {
	name  string
	isDir bool
}

func (m mockDirEntry) Name() string               { return m.name }
func (m mockDirEntry) IsDir() bool                { return m.isDir }
func (m mockDirEntry) Type() os.FileMode          { return 0 }
func (m mockDirEntry) Info() (os.FileInfo, error) { return nil, nil }

func TestTransparentMemoryOffloading_GetAdvices_DyingMemcgReclaim(t *testing.T) {
	defer mockey.UnPatchAll()

	tmo := &transparentMemoryOffloading{}

	mockey.Mock(os.ReadDir).Return([]os.DirEntry{
		mockDirEntry{name: "offline-besteffort-0", isDir: true},
		mockDirEntry{name: "offline-besteffort-1", isDir: true},
		mockDirEntry{name: "burstable", isDir: true},
	}, nil).Build()

	result := tmo.GetAdvices()
	assert.Len(t, result.ExtraEntries, 3)

	expectedPaths := map[string]struct{}{
		"/" + memoryadvisor.OnlineBurstableCgroupPath:                    {},
		"/" + memoryadvisor.KubePodsCgroupPath + "/offline-besteffort-0": {},
		"/" + memoryadvisor.KubePodsCgroupPath + "/offline-besteffort-1": {},
	}

	for _, entry := range result.ExtraEntries {
		assert.Equal(t, consts.ControlKnobON, entry.Values[string(memoryadvisor.ControlKnowKeyDyingMemcgReclaim)])
		_, ok := expectedPaths[entry.CgroupPath]
		assert.True(t, ok)
		delete(expectedPaths, entry.CgroupPath)
	}
	assert.Empty(t, expectedPaths)
}

func TestTransparentMemoryOffloading_GetAdvices_DyingMemcgReclaimInterval(t *testing.T) {
	defer mockey.UnPatchAll()

	tmo := &transparentMemoryOffloading{
		lastDyingCGReclaimTime: time.Now(),
	}

	mockey.Mock(os.ReadDir).Return([]os.DirEntry{
		mockDirEntry{name: "offline-besteffort-0", isDir: true},
	}, nil).Build()

	result := tmo.GetAdvices()
	assert.Len(t, result.ExtraEntries, 0)
}

func TestTransparentMemoryOffloading_GetAdvices_DyingMemcgReclaimReadDirError(t *testing.T) {
	defer mockey.UnPatchAll()

	tmo := &transparentMemoryOffloading{}

	mockey.Mock(os.ReadDir).Return(nil, errors.New("read failed")).Build()

	result := tmo.GetAdvices()
	assert.Len(t, result.ExtraEntries, 0)
}
