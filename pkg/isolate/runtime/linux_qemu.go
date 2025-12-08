//go:build linux

package runtime

func init() {
	Register(Descriptor{
		Name:       "linux-qemu-kvm",
		OS:         "linux",
		Hypervisor: "qemu-kvm",
		Priority:   30,
		Notes:      "QEMU/KVM universal fallback",
	}, func() Runtime {
		return newStubRuntime(Descriptor{
			Name:       "linux-qemu-kvm",
			OS:         "linux",
			Hypervisor: "qemu-kvm",
			Priority:   30,
			Notes:      "QEMU/KVM stub",
		}, "qemu-system-x86_64", "qemu-system-aarch64")
	})
}
