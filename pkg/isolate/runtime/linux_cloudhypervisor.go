//go:build linux

package runtime

func init() {
	Register(Descriptor{
		Name:       "linux-cloud-hypervisor",
		OS:         "linux",
		Hypervisor: "cloud-hypervisor",
		Priority:   20,
		Notes:      "Cloud Hypervisor fallback",
	}, func() Runtime {
		return newStubRuntime(Descriptor{
			Name:       "linux-cloud-hypervisor",
			OS:         "linux",
			Hypervisor: "cloud-hypervisor",
			Priority:   20,
			Notes:      "Cloud Hypervisor stub",
		}, "cloud-hypervisor")
	})
}
