package service

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/meshploy/packages/db"
	appk8s "github.com/meshploy/packages/server/k8s"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// pullWatch follows a node downloading a build's image, so the build log can
// say how far it has got. The cluster says only that a pull is under way, so
// the progress is an estimate: the image's size from its registry, against
// what the node has received since the download began, from its
// node_exporter. Anything else the node receives meanwhile counts too, which
// is why the log says "about".
type pullWatch struct {
	mu      sync.Mutex
	image   string
	node    string
	size    int64     // bytes to download; 0 when the registry did not say
	since   time.Time // when the download began
	partial bool      // the watch began well after the download did
	nodeID  uuid.UUID // the node's row; uuid.Nil when none matched
	samples []rxSample
	pulling bool
}

type rxSample struct {
	at time.Time
	rx int64
}

// pullRateWindow is how far back the download rate is measured.
const pullRateWindow = 30 * time.Second

// followPull watches a build job's pod until its container starts, recording
// any image download on the way. It runs beside followBuild, which owns the
// job, so a download is followed from its start whether or not anyone is
// reading the log; the stream reads the watch through pullFor.
func (s *DeploymentService) followPull(ctx context.Context, namespace, jobName string) *pullTracker {
	ctx, cancel := context.WithCancel(ctx)
	t := &pullTracker{stop: cancel}
	if s.k8s == nil {
		return t
	}
	go func() {
		defer s.pulls.Delete(jobName)
		tick := time.NewTicker(5 * time.Second)
		defer tick.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-tick.C:
			}
			pod := appk8s.JobPod(ctx, s.k8s, namespace, jobName)
			if pod == nil {
				continue
			}
			st := appk8s.PodStarting(ctx, s.k8s, namespace, pod.Name)
			if st.Started {
				t.set(nil)
				return
			}
			w := t.get()
			if !st.Pulling {
				if w != nil {
					w.mu.Lock()
					w.pulling = false
					w.mu.Unlock()
				}
				continue
			}
			if w == nil || w.image != st.Image || w.node != st.Node {
				w = s.startPullWatch(ctx, st)
				t.set(w)
				s.pulls.Store(jobName, w)
			}
			w.sample(ctx, s.nodes)
		}
	}()
	return t
}

// pullTracker is followPull's handle: stop ends the watch, and the last
// watch says whether the build was still downloading when it ended.
type pullTracker struct {
	stop func()
	mu   sync.Mutex
	w    *pullWatch
}

func (t *pullTracker) get() *pullWatch {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.w
}

func (t *pullTracker) set(w *pullWatch) {
	t.mu.Lock()
	t.w = w
	t.mu.Unlock()
}

// stillPulling is the watch when its download had not finished.
func (t *pullTracker) stillPulling() *pullWatch {
	w := t.get()
	if w == nil {
		return nil
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.pulling {
		return nil
	}
	return w
}

// pullFor is the download a build job's pod is waiting on, or nil.
func (s *DeploymentService) pullFor(jobName string) *pullWatch {
	if v, ok := s.pulls.Load(jobName); ok {
		return v.(*pullWatch)
	}
	return nil
}

func (s *DeploymentService) startPullWatch(ctx context.Context, st appk8s.PodStart) *pullWatch {
	now := time.Now()
	w := &pullWatch{image: st.Image, node: st.Node, since: now, pulling: true}
	if !st.PullingSince.IsZero() && st.PullingSince.Before(now) {
		w.since = st.PullingSince
		w.partial = now.Sub(st.PullingSince) > 20*time.Second
	}
	osName, arch, ip := "linux", "amd64", ""
	if kn, err := s.k8s.CoreV1().Nodes().Get(ctx, st.Node, metav1.GetOptions{}); err == nil {
		if kn.Status.NodeInfo.OperatingSystem != "" {
			osName = kn.Status.NodeInfo.OperatingSystem
		}
		if kn.Status.NodeInfo.Architecture != "" {
			arch = kn.Status.NodeInfo.Architecture
		}
		for _, a := range kn.Status.Addresses {
			if a.Type == corev1.NodeInternalIP {
				ip = a.Address
			}
		}
	}
	sizeCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	w.size, _ = imageDownloadSize(sizeCtx, st.Image, osName, arch)
	cancel()
	// A Meshploy node is the cluster node on its mesh address, or of its name.
	var n db.Node
	if ip != "" && s.db.WithContext(ctx).Select("id").Where("tailscale_ip = ?", ip).Limit(1).Find(&n).RowsAffected == 1 {
		w.nodeID = n.ID
	} else if s.db.WithContext(ctx).Select("id").Where("name = ?", st.Node).Limit(1).Find(&n).RowsAffected == 1 {
		w.nodeID = n.ID
	}
	return w
}

// sample reads what the node has received so far.
func (w *pullWatch) sample(ctx context.Context, nodes *NodeService) {
	if nodes == nil || w.nodeID == uuid.Nil {
		return
	}
	m, err := nodes.GetNodeMetrics(ctx, w.nodeID)
	if err != nil {
		return
	}
	w.add(time.Now(), m.NetRxBytes)
}

func (w *pullWatch) add(at time.Time, rx int64) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.pulling = true
	if n := len(w.samples); n > 0 && rx < w.samples[n-1].rx {
		// The counter restarted (the exporter or the node did): what came
		// before is kept as an offset, so the total does not go back.
		delta := w.samples[n-1].rx - rx
		for i := range w.samples {
			w.samples[i].rx -= delta
		}
	}
	w.samples = append(w.samples, rxSample{at: at, rx: rx})
	// The first sample is kept as the start; the rest only as far back as
	// the rate needs.
	for len(w.samples) > 2 && at.Sub(w.samples[1].at) > pullRateWindow {
		w.samples = append(w.samples[:1], w.samples[2:]...)
	}
}

// progress is the download so far: bytes received since the watch began, and
// the rate over the last half minute. ok is false before two samples.
func (w *pullWatch) progress() (got int64, rate float64, ok bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	n := len(w.samples)
	if n < 2 {
		return 0, 0, false
	}
	first, last := w.samples[0], w.samples[n-1]
	from := w.samples[1]
	if n == 2 {
		from = first
	}
	if dt := last.at.Sub(from.at).Seconds(); dt > 0 {
		rate = float64(last.rx-from.rx) / dt
	}
	return last.rx - first.rx, rate, true
}

// left is the estimated time until the download ends; ok is false when it
// cannot be told.
func (w *pullWatch) left() (time.Duration, bool) {
	got, rate, ok := w.progress()
	if !ok || w.size == 0 || w.partial || rate <= 0 {
		return 0, false
	}
	remaining := w.size - got
	if remaining < 0 {
		remaining = 0
	}
	return time.Duration(float64(remaining)/rate) * time.Second, true
}

// line says, for the build log, how the download is going. "" when there is
// nothing to say beyond that it is under way.
func (w *pullWatch) line() string {
	got, rate, ok := w.progress()
	if !ok {
		if w.size > 0 {
			return fmt.Sprintf("Downloading %s", pullBytes(w.size))
		}
		return ""
	}
	elapsed := time.Since(w.since).Round(time.Second)
	switch {
	case w.size > 0 && !w.partial:
		if got > w.size*99/100 {
			got = w.size * 99 / 100
		}
		if left, ok := w.left(); ok {
			return fmt.Sprintf("Downloaded about %s of %s at %s, about %s to go (%s)",
				pullBytes(got), pullBytes(w.size), pullRate(rate), pullDuration(left), elapsed)
		}
		return fmt.Sprintf("Downloaded about %s of %s; nothing arrived in the last half minute (%s)",
			pullBytes(got), pullBytes(w.size), elapsed)
	case w.size > 0:
		return fmt.Sprintf("Downloading %s at %s (%s)", pullBytes(w.size), pullRate(rate), elapsed)
	default:
		return fmt.Sprintf("Downloading at %s (%s)", pullRate(rate), elapsed)
	}
}

// outlasts reports a download that, at its current rate, ends after limit
// has passed since it began: the build will be stopped before it starts.
func (w *pullWatch) outlasts(limit time.Duration) bool {
	left, ok := w.left()
	return ok && time.Since(w.since)+left > limit
}

func pullBytes(b int64) string {
	switch {
	case b >= 1<<30:
		return fmt.Sprintf("%.1f GB", float64(b)/(1<<30))
	case b >= 1<<20:
		return fmt.Sprintf("%.0f MB", float64(b)/(1<<20))
	default:
		return fmt.Sprintf("%.0f KB", float64(b)/(1<<10))
	}
}

func pullRate(r float64) string {
	if r >= 1<<20 {
		return fmt.Sprintf("%.1f MB/s", r/(1<<20))
	}
	return fmt.Sprintf("%.0f KB/s", r/(1<<10))
}

func pullDuration(d time.Duration) string {
	switch {
	case d < time.Minute:
		return "under a minute"
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	default:
		return fmt.Sprintf("%dh%02dm", int(d.Hours()), int(d.Minutes())%60)
	}
}

// slowPullMessage says why a build whose image downloads too slowly cannot
// start, and what to do about it.
func slowPullMessage(w *pullWatch) string {
	_, rate, ok := w.progress()
	speed := "slowly"
	if ok && rate > 0 {
		speed = "at " + pullRate(rate)
	}
	return fmt.Sprintf("%s is downloading the image %s, too slowly to finish within the %s a build may run, so the build stops before it starts. "+
		"To build on another node, turn off \"Act as build node\" on %s's page; or deploy again once its connection is faster.",
		w.node, speed, pullDuration(appk8s.BuildDeadline), w.node)
}
