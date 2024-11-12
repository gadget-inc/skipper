```go
func (self *Router) ServeHTTP(rw, req) {
    pod := pods.Get(project)
    self.proxy.ServeHTTP(rw, req)
}

func Get(project) *pod.Pod {
    return timers.Poll(func() *pod.Pod {
        pods := getAssignedPods(project)
        if len(pods) == 0 {
            go assignPodFromPool(project)
            return nil
        }
        for _, pod := range pods {
            if pod.Status == "ready" {
                return pod
            }
        }
        return nil
    })
}

func assignPodFromPool(project) {
    for {
        pod := getAvailablePod(project)

        mut := bigtable.NewMutation()
        mut.Set("info", "status", "pending")
        mut.Set("info", "ip", pod.IP)
        mut.Set("info", "projectId", project.ID)
        mut.Set("info", "assignedAt", time.Now().String())

        err := tbl.Apply("pods#"+pod.Name, mut)
        if err != nil {
            log.Warn("failed to acquire pod in bigtable", slog.Any("error", err))
            continue
        }

        err = k8s.RemovePodFromDeployment(pod, project.ID)
        if err != nil {
            log.Warn("failed to remove pod from deployment", slog.Any("error", err))
            break // rely on convergePods() to remove pod from deployment
        }

        err := callAssignmentEndpoint(pod, project)
        if err != nil {
            log.Warn("failed to assign pod", slog.Any("error", err))

            err = tbl.Apply("pods#" + pod.Name, bigtable.NewMutation().DeleteRow())
            if err != nil {
                log.Warn("failed to delete pod from bigtable", slog.Any("error", err))
                err = nil
            }

            err = k8s.TerminatePod(pod.Name)
            if err != nil {
                log.Warn("failed to terminate pod", slog.Any("error", err))
            }

            continue // rely on convergePods()
        }
    }
}

func (self *pod.Manager) convergePods() {
    var pods []*pod.Pod

    tbl.ReadRows("pods#", func(row bigtable.Row) bool {
        pod := fromBigTableRow(row)
        k8sPod := k8s.GetPod(pod.Name)
        if k8sPod == nil {
            // pod doesn't exist in k8s, so it shouldn't exist in bigtable
            log.Info("deleting pod not found in k8s", slog.Any("pod", pod))
            err := tbl.Apply(row.Row, bigtable.NewMutation().DeleteRow())
            if err != nil {
                log.Warn("failed to delete pod not found in k8s", slog.Any("error", err))
            }
            return true
        }

        k8sProjectID, ok := k8sPod.Labels["fusion/project-id"]
        if !ok {
            // we failed to remove the pod from the deployment
            // delete the pod from bigtable so it can be reassigned
            tbl.Apply(row.Row, bigtable.NewMutation().DeleteRow())
        }

        if k8sProjectID != pod.ProjectID {
            // this should never happen
            log.Warn("pod assigned to different project", slog.Any("pod", pod), slog.Any("k8sPod", k8sPod))

            err := tbl.Apply(row.Row, bigtable.NewMutation().DeleteRow())
            if err != nil {
                log.Warn("failed to delete pod assigned to different project", slog.Any("error", err))
            }

            err = k8s.TerminatePod(pod.Name)
            if err != nil {
                log.Warn("failed to terminate pod assigned to a different project", slog.Any("error", err))
            }

            return true
        }

        if k8sPod.Status != "Running" {
            // only running pods should be in the bigtable
            err := tbl.Apply(row.Row, bigtable.NewMutation().DeleteRow())
            if err != nil {
                log.Warn("failed to delete pod in the process of being terminated", slog.Any("error", err))
            }
            return true
        }

        if pod.Status == "pending" && pod.AssignedAt.Add(1 * time.Minute).Before(time.Now()) {
            log.Warn("pod has been pending for too long", slog.Any("pod", pod))
            err := tbl.Apply(row.Row, bigtable.NewMutation().DeleteRow())
            if err != nil {
                log.Warn("failed to delete pod pending for too long", slog.Any("error", err))
            }

            err = k8s.TerminatePod(pod.Name)
            if err != nil {
                log.Warn("failed to terminate pod pending for too long", slog.Any("error", err))
            }

            return true
        }

        pods = append(pods, pod)
        return true
    })

    for _, k8sPod := range k8s.GetAssignedPods() {
        row := tbl.ReadRow("pods#" + k8sPod.Name)
        if row == nil {
            if k8sPod.Status != "Terminating" {
                // this pod shouldn't exist
                log.Info("terminating pod not found in bigtable", slog.Any("pod", k8sPod))
                err = k8s.TerminatePod(k8sPod.Name)
                if err != nil {
                    log.Warn("failed to terminate pod not found in bigtable", slog.Any("error", err))
                }
            }
        }
    }
}
```
