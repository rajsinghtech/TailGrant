package grant

import (
	"testing"

	"go.temporal.io/sdk/testsuite"
)

func TestActivities_StructShape(t *testing.T) {
	a := &Activities{}
	if a.TS != nil {
		t.Error("expected nil TS client")
	}
	if a.Temporal != nil {
		t.Error("expected nil Temporal client")
	}
}

func TestActivities_Registration(t *testing.T) {
	testSuite := &testsuite.WorkflowTestSuite{}
	env := testSuite.NewTestActivityEnvironment()

	a := &Activities{}
	env.RegisterActivity(a.GetDevice)
	env.RegisterActivity(a.ListDevices)
	env.RegisterActivity(a.GetDeviceTags)
	env.RegisterActivity(a.SetDeviceTags)
	env.RegisterActivity(a.CheckWorkflowExists)
	env.RegisterActivity(a.SignalWithStartDeviceTagManager)
	env.RegisterActivity(a.QueryActiveGrants)
	env.RegisterActivity(a.SetPostureAttribute)
	env.RegisterActivity(a.DeletePostureAttribute)
	env.RegisterActivity(a.GetPostureAttributes)
}
