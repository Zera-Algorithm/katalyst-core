package dynamicpolicy

import (
	"reflect"
	"testing"

	"github.com/bytedance/mockey"
	"github.com/stretchr/testify/assert"

	"github.com/kubewharf/katalyst-core/pkg/agent/qrm-plugins/advisorsvc"
	"github.com/kubewharf/katalyst-core/pkg/agent/qrm-plugins/memory/dynamicpolicy/memoryadvisor"
	"github.com/kubewharf/katalyst-core/pkg/agent/qrm-plugins/util"
	"github.com/kubewharf/katalyst-core/pkg/consts"
	"github.com/kubewharf/katalyst-core/pkg/metrics"
	"github.com/kubewharf/katalyst-core/pkg/util/asyncworker"
	"github.com/kubewharf/katalyst-core/pkg/util/cgroup/common"
	cgroupmgr "github.com/kubewharf/katalyst-core/pkg/util/cgroup/manager"
	"github.com/kubewharf/katalyst-core/pkg/util/machine"
)

func TestDynamicPolicy_handleAdvisorDyingMemcgReclaim(t *testing.T) {
	t.Run("empty cgroup path", func(t *testing.T) {
		p := &DynamicPolicy{}
		calculationInfo := &advisorsvc.CalculationInfo{
			CalculationResult: &advisorsvc.CalculationResult{
				Values: map[string]string{},
			},
		}

		err := p.handleAdvisorDyingMemcgReclaim(nil, nil, nil, metrics.DummyMetrics{}, nil, "pod", "container", calculationInfo, nil)
		assert.Error(t, err)
	})

	t.Run("disabled dying memcg reclaim", func(t *testing.T) {
		defer mockey.UnPatchAll()

		p := &DynamicPolicy{
			defaultAsyncLimitedWorkers: asyncworker.NewAsyncLimitedWorkers("test", 1, metrics.DummyMetrics{}),
		}

		disableSwapCalled := false
		addWorkCalled := false
		getEffectiveCalled := false

		mockey.Mock(cgroupmgr.DisableSwapMaxWithAbsolutePathRecursive).To(func(_ string) error {
			disableSwapCalled = true
			return nil
		}).Build()
		mockey.Mock((*asyncworker.AsyncLimitedWorkers).AddWork).To(func(_ *asyncworker.AsyncLimitedWorkers, _ *asyncworker.Work, _ asyncworker.DuplicateWorkPolicy) error {
			addWorkCalled = true
			return nil
		}).Build()
		mockey.Mock(cgroupmgr.GetEffectiveCPUSetWithAbsolutePath).To(func(_ string) (machine.CPUSet, machine.CPUSet, error) {
			getEffectiveCalled = true
			return machine.NewCPUSet(0), machine.NewCPUSet(0), nil
		}).Build()

		calculationInfo := &advisorsvc.CalculationInfo{
			CgroupPath: "kubepods/burstable",
			CalculationResult: &advisorsvc.CalculationResult{
				Values: map[string]string{
					string(memoryadvisor.ControlKnobKeySwapMax):           consts.ControlKnobOFF,
					string(memoryadvisor.ControlKnowKeyDyingMemcgReclaim): consts.ControlKnobOFF,
				},
			},
		}

		err := p.handleAdvisorDyingMemcgReclaim(nil, nil, nil, metrics.DummyMetrics{}, nil, "pod", "container", calculationInfo, nil)
		assert.NoError(t, err)
		assert.True(t, disableSwapCalled)
		assert.False(t, addWorkCalled)
		assert.False(t, getEffectiveCalled)
	})

	t.Run("enabled dying memcg reclaim", func(t *testing.T) {
		defer mockey.UnPatchAll()

		p := &DynamicPolicy{
			defaultAsyncLimitedWorkers: asyncworker.NewAsyncLimitedWorkers("test", 1, metrics.DummyMetrics{}),
		}

		setSwapCalled := false
		var capturedWork *asyncworker.Work
		var capturedPolicy asyncworker.DuplicateWorkPolicy
		mems := machine.NewCPUSet(0, 1)

		mockey.Mock(cgroupmgr.SetSwapMaxWithAbsolutePathRecursive).To(func(_ string) error {
			setSwapCalled = true
			return nil
		}).Build()
		mockey.Mock(cgroupmgr.GetEffectiveCPUSetWithAbsolutePath).To(func(_ string) (machine.CPUSet, machine.CPUSet, error) {
			return machine.NewCPUSet(0), mems, nil
		}).Build()
		mockey.Mock((*asyncworker.AsyncLimitedWorkers).AddWork).To(func(_ *asyncworker.AsyncLimitedWorkers, work *asyncworker.Work, policy asyncworker.DuplicateWorkPolicy) error {
			capturedWork = work
			capturedPolicy = policy
			return nil
		}).Build()

		calculationInfo := &advisorsvc.CalculationInfo{
			CgroupPath: "kubepods/burstable",
			CalculationResult: &advisorsvc.CalculationResult{
				Values: map[string]string{
					string(memoryadvisor.ControlKnobKeySwapMax):           consts.ControlKnobON,
					string(memoryadvisor.ControlKnowKeyDyingMemcgReclaim): consts.ControlKnobON,
				},
			},
		}

		emitter := metrics.DummyMetrics{}
		err := p.handleAdvisorDyingMemcgReclaim(nil, nil, nil, emitter, nil, "pod", "container", calculationInfo, nil)
		assert.NoError(t, err)
		assert.True(t, setSwapCalled)
		assert.NotNil(t, capturedWork)

		expectedAbsPath := common.GetAbsCgroupPath(common.CgroupSubsysMemory, "kubepods/burstable")
		expectedWorkName := util.GetCgroupAsyncWorkName("kubepods/burstable", memoryPluginAsyncWorkTopicDyingMemcgReclaim)
		assert.Equal(t, expectedWorkName, capturedWork.Name)
		assert.Equal(t, asyncworker.DuplicateWorkPolicy(asyncworker.DuplicateWorkPolicyOverride), capturedPolicy)
		assert.Equal(t, expectedAbsPath, capturedWork.Params[0])
		assert.Equal(t, emitter, capturedWork.Params[1])
		assert.Equal(t, "pod", capturedWork.Params[2])
		assert.Equal(t, "container", capturedWork.Params[3])
		assert.Equal(t, mems, capturedWork.Params[4])
		assert.Equal(t, reflect.ValueOf(cgroupmgr.DyingMemcgReclaimWithAbsolutePath).Pointer(), reflect.ValueOf(capturedWork.Fn).Pointer())
	})
}
