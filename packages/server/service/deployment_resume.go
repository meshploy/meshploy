package service

import (
	"context"
	"log"
	"time"

	db "github.com/meshploy/packages/db"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Deployments are followed by the API process that started them: a build's
// wait, a rollout's watch. A restart - an upgrade, from the console or
// server-upgrade - ended those, and left each deployment in flight saying
// "pending" or "deploying" for good, with nothing to record how its build or
// rollout went, however it went. ResumeInFlight picks each one up where the
// cluster has it, or ends it saying why.

// ResumeInFlight follows again the deployments an API restart left in flight.
func (s *DeploymentService) ResumeInFlight(ctx context.Context) {
	if s.k8s == nil {
		return
	}
	var deps []db.Deployment
	if err := s.db.WithContext(ctx).
		Where("status IN ?", []db.DeploymentStatus{db.DeploymentPending, db.DeploymentBuilding, db.DeploymentDeploying}).
		Find(&deps).Error; err != nil {
		log.Printf("resume deployments: %v", err)
		return
	}
	for _, d := range deps {
		go s.resume(context.WithoutCancel(ctx), d)
	}
	if len(deps) > 0 {
		log.Printf("resume deployments: following %d again after the restart", len(deps))
	}
}

// restartNote opens what a resumed deployment's log says next.
const restartNote = "\nThe API restarted while this deployment was in flight; "

func (s *DeploymentService) resume(ctx context.Context, d db.Deployment) {
	var svc db.Service
	if err := s.db.WithContext(ctx).Preload("Project").Preload("Ports").First(&svc, "id = ?", d.ServiceID).Error; err != nil {
		s.failDeployment(d.ID, d.Log+restartNote+"its service is gone.")
		return
	}
	namespace := svc.Project.Slug

	switch {
	case d.BuildJobName != "" && (d.Status == db.DeploymentPending || d.Status == db.DeploymentBuilding):
		// The job goes on in the cluster whatever the API does: follow it again.
		if _, err := s.k8s.BatchV1().Jobs(namespace).Get(ctx, d.BuildJobName, metav1.GetOptions{}); err != nil {
			s.failDeployment(d.ID, d.Log+restartNote+"its build job is gone. Deploy it again.")
			return
		}
		var bc db.BuildConfig
		if err := s.db.WithContext(ctx).Preload("RegistryIntegration").Where("service_id = ?", svc.ID).First(&bc).Error; err != nil {
			s.failDeployment(d.ID, d.Log+restartNote+"its build configuration is gone.")
			return
		}
		host, user, pass, err := s.resolveRegistry(ctx, &bc)
		if err != nil {
			s.failDeployment(d.ID, d.Log+restartNote+"its registry: "+err.Error())
			return
		}
		s.setStatus(d.ID, d.Status, d.Log+restartNote+"following its build again.")
		d.Log += restartNote + "following its build again.\n"
		s.followBuild(ctx, runPipelineArgs{
			deployment: d, svc: svc, bc: bc, namespace: namespace,
			jobName: d.BuildJobName, imageName: d.Image,
			registryHost: host, registryUser: user, registryPass: pass,
		})

	case d.Status == db.DeploymentDeploying && svc.Type == db.ServiceTypeApplication:
		// Only what reached the cluster can be watched: a workload still on
		// an older image means the restart came before it was applied.
		applied, err := s.appliedImage(ctx, &svc, namespace)
		if err != nil || applied != d.Image {
			s.failDeployment(d.ID, d.Log+restartNote+"it had not reached the cluster yet. Deploy it again.")
			s.db.WithContext(ctx).Model(&db.Service{}).Where("id = ? AND status = ?", svc.ID, db.ServiceDeploying).
				Update("status", db.ServiceFailed)
			return
		}
		logText := d.Log + restartNote + "following its rollout again.\n"
		s.appendLog(d.ID, &logText, "")
		res := s.awaitWorkload(ctx, &svc, namespace, func(line string) { s.appendLog(d.ID, &logText, line) })
		if !res.Succeeded {
			s.failDeployment(d.ID, logText+"\nRollout failed: "+res.Reason)
			s.db.WithContext(ctx).Model(&db.Service{}).Where("id = ?", svc.ID).Update("status", db.ServiceFailed)
			return
		}
		s.markDeployed(ctx, &svc, d.Image)
		s.succeedDeployment(ctx, d.ID, svc.ID, logText, d.Image)

	default:
		// A database being provisioned, or a deployment that never got as
		// far as a job: nothing in the cluster to follow.
		s.failDeployment(d.ID, d.Log+restartNote+"there is nothing in the cluster to follow. Deploy it again.")
	}
}

// appliedImage is the image the service's workload in the cluster runs.
func (s *DeploymentService) appliedImage(ctx context.Context, svc *db.Service, namespace string) (string, error) {
	name := appK8sName(svc)
	if svc.RunOnce {
		job, err := s.k8s.BatchV1().Jobs(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return "", err
		}
		return firstImage(job.Spec.Template.Spec.Containers), nil
	}
	dep, err := s.k8s.AppsV1().Deployments(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return "", err
	}
	return firstImage(dep.Spec.Template.Spec.Containers), nil
}

func firstImage(containers []corev1.Container) string {
	if len(containers) == 0 {
		return ""
	}
	return containers[0].Image
}

// resumeAfter is how long after the API starts in-flight work is picked up,
// so the reconcilers that start with it have run once.
var resumeAfter = 5 * time.Second
