package service

import "testing"

// node_exporter on macOS names memory differently and mounts the disk that
// fills up somewhere other than /. Read with Linux's names alone, a Mac showed
// no available memory at all and the size of its read-only system volume.
func TestMacMetricsAreReadWithMacNames(t *testing.T) {
	body := []byte(`# HELP node_cpu_seconds_total Seconds the CPUs spent in each mode.
node_cpu_seconds_total{cpu="0",mode="idle"} 900
node_cpu_seconds_total{cpu="0",mode="user"} 100
node_cpu_seconds_total{cpu="1",mode="idle"} 800
node_cpu_seconds_total{cpu="1",mode="user"} 200
node_memory_total_bytes 17179869184
node_memory_free_bytes 1073741824
node_memory_inactive_bytes 4294967296
node_memory_purgeable_bytes 536870912
node_memory_wired_bytes 2147483648
node_filesystem_size_bytes{device="/dev/disk3s1s1",fstype="apfs",mountpoint="/"} 994662584320
node_filesystem_avail_bytes{device="/dev/disk3s1s1",fstype="apfs",mountpoint="/"} 500000000000
node_filesystem_size_bytes{device="/dev/disk3s5",fstype="apfs",mountpoint="/System/Volumes/Data"} 994662584320
node_filesystem_avail_bytes{device="/dev/disk3s5",fstype="apfs",mountpoint="/System/Volumes/Data"} 123456789012
node_network_receive_bytes_total{device="lo0"} 999999
node_network_receive_bytes_total{device="en0"} 5000
node_network_transmit_bytes_total{device="lo0"} 999999
node_network_transmit_bytes_total{device="en0"} 7000
`)
	m, err := parseNodeExporterBody(body)
	if err != nil {
		t.Fatal(err)
	}
	if m.CPUCores != 2 {
		t.Errorf("cores = %d, want 2", m.CPUCores)
	}
	if m.MemoryTotalBytes != 17179869184 {
		t.Errorf("memory total = %d", m.MemoryTotalBytes)
	}
	// free + inactive + purgeable: what Activity Monitor calls available.
	if want := int64(1073741824 + 4294967296 + 536870912); m.MemoryAvailableBytes != want {
		t.Errorf("memory available = %d, want %d", m.MemoryAvailableBytes, want)
	}
	if m.DiskAvailBytes != 123456789012 {
		t.Errorf("disk available = %d, want the data volume's", m.DiskAvailBytes)
	}
	if m.NetRxBytes != 5000 || m.NetTxBytes != 7000 {
		t.Errorf("network = %d/%d, want lo0 left out", m.NetRxBytes, m.NetTxBytes)
	}
}

// And Linux is read exactly as before: MemAvailable wins, / is the disk.
func TestLinuxMetricsAreUnchanged(t *testing.T) {
	body := []byte(`node_memory_MemTotal_bytes 8000
node_memory_MemAvailable_bytes 3000
node_filesystem_size_bytes{fstype="ext4",mountpoint="/"} 100
node_filesystem_avail_bytes{fstype="ext4",mountpoint="/"} 40
node_network_receive_bytes_total{device="lo"} 999
node_network_receive_bytes_total{device="eth0"} 10
`)
	m, err := parseNodeExporterBody(body)
	if err != nil {
		t.Fatal(err)
	}
	if m.MemoryTotalBytes != 8000 || m.MemoryAvailableBytes != 3000 {
		t.Errorf("memory = %d/%d", m.MemoryTotalBytes, m.MemoryAvailableBytes)
	}
	if m.DiskTotalBytes != 100 || m.DiskAvailBytes != 40 {
		t.Errorf("disk = %d/%d", m.DiskTotalBytes, m.DiskAvailBytes)
	}
	if m.NetRxBytes != 10 {
		t.Errorf("rx = %d", m.NetRxBytes)
	}
}
