package mng

import (
	"context"
	"fmt"
	"io/ioutil"
	"math/rand"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/aws/aws-k8s-tester/ec2config"
	"github.com/aws/aws-k8s-tester/pkg/fileutil"
	"github.com/aws/aws-k8s-tester/pkg/ssh"
	"go.uber.org/zap"
	"golang.org/x/time/rate"
)

var logCmds = map[string]string{
	// kernel logs
	"sudo journalctl --no-pager --output=short-precise -k": "kernel.out.log",

	// full journal logs (e.g. disk mounts)
	"sudo journalctl --no-pager --output=short-precise": "journal.out.log",

	// other systemd services
	"sudo systemctl list-units -t service --no-pager --no-legend --all": "list-units-systemctl.out.log",
}

// FetchLogs downloads logs from managed node group instances.
func (ts *tester) FetchLogs() (err error) {
	if !ts.cfg.EKSConfig.AddOnManagedNodeGroups.Enable {
		return fmt.Errorf("AddOnManagedNodeGroups.Enable %v; no managed node group to fetch logs for", ts.cfg.EKSConfig.AddOnManagedNodeGroups.Enable)
	}
	if err := os.MkdirAll(ts.cfg.EKSConfig.AddOnManagedNodeGroups.LogDir, 0700); err != nil {
		return err
	}
	ts.logsMu.Lock()
	defer ts.logsMu.Unlock()
	return ts.fetchLogs(150, 10, logCmds)
}

// only letters and numbers
var regex = regexp.MustCompile("[^a-zA-Z0-9]+")

func (ts *tester) fetchLogs(qps float32, burst int, commandToFileName map[string]string) error {
	logsDir, err := ioutil.TempDir(
		ts.cfg.EKSConfig.AddOnManagedNodeGroups.LogDir,
		ts.cfg.EKSConfig.Name+"-mng-logs",
	)
	if err != nil {
		return err
	}

	sshOpt := ssh.WithVerbose(ts.cfg.EKSConfig.LogLevel == "debug")
	rateLimiter := rate.NewLimiter(rate.Limit(qps), burst)
	rch, waits := make(chan instanceLogs, 10), 0

	for name, nodeGroup := range ts.cfg.EKSConfig.StatusManagedNodeGroups.Nodes {
		ts.cfg.Logger.Info("fetching logs",
			zap.String("mng-name", name),
			zap.Int("nodes", len(nodeGroup.Instances)),
		)
		waits += len(nodeGroup.Instances)

		for instID, iv := range nodeGroup.Instances {
			dns := strings.ToLower(regex.ReplaceAllString(iv.PublicDNSName, ""))
			dns = strings.ReplaceAll(dns, "ec2", "")
			dns = strings.ReplaceAll(dns, "compute.amazonaws", "")
			dns = strings.ReplaceAll(dns, "amazonaws", "")
			if len(dns) > 7 {
				dns = dns[:7]
			}
			pfx := instID + "-" + dns + "-"

			go func(instID, logsDir, pfx string, iv ec2config.Instance) {
				select {
				case <-ts.cfg.Stopc:
					ts.cfg.Logger.Warn("exiting fetch logger", zap.String("prefix", pfx))
					return
				default:
				}

				if !rateLimiter.Allow() {
					ts.cfg.Logger.Debug("waiting for rate limiter before SSH into the machine",
						zap.Float32("qps", qps),
						zap.Int("burst", burst),
						zap.String("instance-id", instID),
					)
					werr := rateLimiter.Wait(context.Background())
					ts.cfg.Logger.Debug("waited for rate limiter",
						zap.Float32("qps", qps),
						zap.Int("burst", burst),
						zap.Error(werr),
					)
				}

				sh, err := ssh.New(ssh.Config{
					Logger:        ts.cfg.Logger,
					KeyPath:       ts.cfg.EKSConfig.AddOnManagedNodeGroups.RemoteAccessPrivateKeyPath,
					PublicIP:      iv.PublicIP,
					PublicDNSName: iv.PublicDNSName,
					UserName:      ts.cfg.EKSConfig.AddOnManagedNodeGroups.RemoteAccessUserName,
				})
				if err != nil {
					rch <- instanceLogs{mngName: name, err: err}
					return
				}
				defer sh.Close()
				if err = sh.Connect(); err != nil {
					rch <- instanceLogs{mngName: name, err: err}
					return
				}

				data := instanceLogs{mngName: name, instanceID: instID}

				// fetch default logs
				for cmd, fileName := range commandToFileName {
					if !rateLimiter.Allow() {
						ts.cfg.Logger.Debug("waiting for rate limiter before fetching file")
						werr := rateLimiter.Wait(context.Background())
						ts.cfg.Logger.Debug("waited for rate limiter", zap.Error(werr))
					}
					out, oerr := sh.Run(cmd, sshOpt)
					if oerr != nil {
						rch <- instanceLogs{
							mngName:    name,
							instanceID: instID,
							err: fmt.Errorf(
								"failed to run command %q for %q (error %v)",
								cmd,
								instID,
								oerr,
							)}
						return
					}

					fpath := filepath.Join(logsDir, shorten(ts.cfg.Logger, pfx+fileName))
					f, err := os.Create(fpath)
					if err != nil {
						rch <- instanceLogs{
							mngName:    name,
							instanceID: instID,
							err: fmt.Errorf(
								"failed to create a file %q for %q (error %v)",
								fpath,
								instID,
								err,
							)}
						return
					}
					if _, err = f.Write(out); err != nil {
						rch <- instanceLogs{
							mngName:    name,
							instanceID: instID,
							err: fmt.Errorf(
								"failed to write to a file %q for %q (error %v)",
								fpath,
								instID,
								err,
							)}
						f.Close()
						return
					}
					f.Close()
					ts.cfg.Logger.Debug("wrote", zap.String("file-path", fpath))
					data.paths = append(data.paths, fpath)
				}

				if !rateLimiter.Allow() {
					ts.cfg.Logger.Debug("waiting for rate limiter before fetching file")
					werr := rateLimiter.Wait(context.Background())
					ts.cfg.Logger.Debug("waited for rate limiter", zap.Error(werr))
				}
				ts.cfg.Logger.Info("listing systemd service units", zap.String("instance-id", instID))
				listCmd := "sudo systemctl list-units -t service --no-pager --no-legend --all"
				out, oerr := sh.Run(listCmd, sshOpt)
				if oerr != nil {
					rch <- instanceLogs{
						mngName:    name,
						instanceID: instID,
						err: fmt.Errorf(
							"failed to run command %q for %q (error %v)",
							listCmd,
							instID,
							oerr,
						)}
					return
				}
				/*
					auditd.service                                        loaded    active   running Security Auditing Service
					auth-rpcgss-module.service                            loaded    inactive dead    Kernel Module supporting RPCSEC_GSS
				*/
				svcCmdToFileName := make(map[string]string)
				for _, line := range strings.Split(string(out), "\n") {
					fields := strings.Fields(line)
					if len(fields) == 0 || fields[0] == "" || len(fields) < 5 {
						continue
					}
					if fields[1] == "not-found" {
						continue
					}
					if fields[2] == "inactive" {
						continue
					}
					svc := fields[0]
					svcCmd := "sudo journalctl --no-pager --output=cat -u " + svc
					svcFileName := svc + ".out.log"
					svcCmdToFileName[svcCmd] = svcFileName
				}
				for cmd, fileName := range svcCmdToFileName {
					if !rateLimiter.Allow() {
						ts.cfg.Logger.Debug("waiting for rate limiter before fetching file")
						werr := rateLimiter.Wait(context.Background())
						ts.cfg.Logger.Debug("waited for rate limiter", zap.Error(werr))
					}
					out, oerr := sh.Run(cmd, sshOpt)
					if oerr != nil {
						rch <- instanceLogs{
							mngName:    name,
							instanceID: instID,
							err: fmt.Errorf(
								"failed to run command %q for %q (error %v)",
								cmd,
								instID,
								oerr,
							)}
						return
					}

					fpath := filepath.Join(logsDir, shorten(ts.cfg.Logger, pfx+fileName))
					f, err := os.Create(fpath)
					if err != nil {
						rch <- instanceLogs{
							mngName:    name,
							instanceID: instID,
							err: fmt.Errorf(
								"failed to create a file %q for %q (error %v)",
								fpath,
								instID,
								err,
							)}
						return
					}
					if _, err = f.Write(out); err != nil {
						rch <- instanceLogs{
							mngName:    name,
							instanceID: instID,
							err: fmt.Errorf(
								"failed to write to a file %q for %q (error %v)",
								fpath,
								instID,
								err,
							)}
						f.Close()
						return
					}
					f.Close()
					ts.cfg.Logger.Debug("wrote", zap.String("file-path", fpath))
					data.paths = append(data.paths, fpath)
				}

				if !rateLimiter.Allow() {
					ts.cfg.Logger.Debug("waiting for rate limiter before fetching file")
					werr := rateLimiter.Wait(context.Background())
					ts.cfg.Logger.Debug("waited for rate limiter", zap.Error(werr))
				}
				// https://github.com/aws/amazon-vpc-cni-k8s/blob/master/docs/troubleshooting.md#ipamd-debugging-commands
				ts.cfg.Logger.Info("fetching ENI information", zap.String("instance-id", instID))
				eniCmd := "curl http://localhost:61679/v1/enis"
				out, oerr = sh.Run(eniCmd, sshOpt)
				if oerr != nil {
					rch <- instanceLogs{
						mngName:    name,
						instanceID: instID,
						err: fmt.Errorf(
							"failed to run command %q for %q (error %v)",
							eniCmd,
							instID,
							oerr,
						)}
					return
				}
				v1ENIOutputPath := filepath.Join(logsDir, shorten(ts.cfg.Logger, pfx+"v1-enis"))
				f, err := os.Create(v1ENIOutputPath)
				if err != nil {
					rch <- instanceLogs{
						mngName:    name,
						instanceID: instID,
						err: fmt.Errorf(
							"failed to create a file %q for %q (error %v)",
							v1ENIOutputPath,
							instID,
							err,
						)}
					return
				}
				if _, err = f.Write(out); err != nil {
					rch <- instanceLogs{
						mngName:    name,
						instanceID: instID,
						err: fmt.Errorf(
							"failed to write to a file %q for %q (error %v)",
							v1ENIOutputPath,
							instID,
							err,
						)}
					f.Close()
					return
				}
				f.Close()
				ts.cfg.Logger.Debug("wrote", zap.String("file-path", v1ENIOutputPath))
				data.paths = append(data.paths, v1ENIOutputPath)

				if !rateLimiter.Allow() {
					ts.cfg.Logger.Debug("waiting for rate limiter before fetching file")
					werr := rateLimiter.Wait(context.Background())
					ts.cfg.Logger.Debug("waited for rate limiter", zap.Error(werr))
				}
				ts.cfg.Logger.Info("listing /var/log", zap.String("instance-id", instID))
				findCmd := "sudo find /var/log ! -type d"
				out, oerr = sh.Run(findCmd, sshOpt)
				if oerr != nil {
					rch <- instanceLogs{
						mngName:    name,
						instanceID: instID,
						err: fmt.Errorf(
							"failed to run command %q for %q (error %v)",
							findCmd,
							instID,
							oerr,
						)}
					return
				}
				varLogCmdToFileName := make(map[string]string)
				for _, line := range strings.Split(string(out), "\n") {
					if len(line) == 0 {
						// last value
						continue
					}
					logCmd := "sudo cat " + line
					logName := filepath.Base(line)
					varLogCmdToFileName[logCmd] = logName
				}
				for cmd, fileName := range varLogCmdToFileName {
					if !rateLimiter.Allow() {
						ts.cfg.Logger.Debug("waiting for rate limiter before fetching file")
						werr := rateLimiter.Wait(context.Background())
						ts.cfg.Logger.Debug("waited for rate limiter", zap.Error(werr))
					}
					out, oerr := sh.Run(cmd, sshOpt)
					if oerr != nil {
						rch <- instanceLogs{
							mngName:    name,
							instanceID: instID,
							err: fmt.Errorf(
								"failed to run command %q for %q (error %v)",
								cmd,
								instID,
								oerr,
							)}
						return
					}

					fpath := filepath.Join(logsDir, shorten(ts.cfg.Logger, pfx+fileName))
					f, err := os.Create(fpath)
					if err != nil {
						rch <- instanceLogs{
							mngName:    name,
							instanceID: instID,
							err: fmt.Errorf(
								"failed to create a file %q for %q (error %v)",
								fpath,
								instID,
								err,
							)}
						return
					}
					if _, err = f.Write(out); err != nil {
						rch <- instanceLogs{
							mngName:    name,
							instanceID: instID,
							err: fmt.Errorf(
								"failed to write to a file %q for %q (error %v)",
								fpath,
								instID,
								err,
							)}
						f.Close()
						return
					}
					f.Close()
					ts.cfg.Logger.Debug("wrote", zap.String("file-path", fpath))
					data.paths = append(data.paths, fpath)
				}

				rch <- data
			}(instID, logsDir, pfx, iv)
		}
	}

	total := 0
	for i := 0; i < waits; i++ {
		var data instanceLogs
		select {
		case data = <-rch:
		case <-ts.cfg.Stopc:
			ts.cfg.Logger.Warn("exiting fetch logger")
			return ts.cfg.EKSConfig.Sync()
		}
		if data.err != nil {
			ts.cfg.Logger.Error("failed to fetch logs",
				zap.String("mng-name", data.mngName),
				zap.String("instance-id", data.instanceID),
				zap.Error(data.err),
			)
			continue
		}
		mv, ok := ts.cfg.EKSConfig.StatusManagedNodeGroups.Nodes[data.mngName]
		if !ok {
			return fmt.Errorf("EKS Managed Node Group name %q is unknown", data.mngName)
		}
		if mv.Logs == nil {
			mv.Logs = make(map[string][]string)
		}
		_, ok = mv.Logs[data.instanceID]
		if ok {
			return fmt.Errorf("EKS Managed Node Group name %q for instance %q logs are redundant", data.mngName, data.instanceID)
		}

		mv.Logs[data.instanceID] = data.paths

		ts.cfg.EKSConfig.StatusManagedNodeGroups.Nodes[data.mngName] = mv
		ts.cfg.EKSConfig.Sync()

		files := len(data.paths)
		total += files
		ts.cfg.Logger.Info("wrote log files",
			zap.String("instance-id", data.instanceID),
			zap.Int("files", files),
			zap.Int("total-downloaded-files", total),
		)
	}

	ts.cfg.Logger.Info("wrote all log files",
		zap.String("log-dir", logsDir),
		zap.Int("total-downloaded-files", total),
	)
	return ts.cfg.EKSConfig.Sync()
}

type instanceLogs struct {
	mngName    string
	instanceID string
	paths      []string
	err        error
}

func (ts *tester) DownloadClusterLogs(artifactDir string) error {
	err := ts.FetchLogs()
	if err != nil {
		return err
	}

	ts.logsMu.RLock()
	defer ts.logsMu.RUnlock()

	for _, v := range ts.cfg.EKSConfig.StatusManagedNodeGroups.Nodes {
		for _, fpaths := range v.Logs {
			for _, fpath := range fpaths {
				newPath := filepath.Join(artifactDir, filepath.Base(fpath))
				if err := fileutil.Copy(fpath, newPath); err != nil {
					return err
				}
			}
		}
	}

	return fileutil.Copy(
		ts.cfg.EKSConfig.ConfigPath,
		filepath.Join(artifactDir, filepath.Base(ts.cfg.EKSConfig.ConfigPath)),
	)
}

func shorten(lg *zap.Logger, name string) string {
	if len(name) < 240 {
		return name
	}

	ext := filepath.Ext(name)
	oldName := name

	name = name[:230] + randString(5) + ext
	lg.Info("file name too long; renamed", zap.String("old", oldName), zap.String("new", name))
	return name
}

const ll = "0123456789abcdefghijklmnopqrstuvwxyz"

func randString(n int) string {
	b := make([]byte, n)
	for i := range b {
		rand.Seed(time.Now().UnixNano())
		b[i] = ll[rand.Intn(len(ll))]
	}
	return string(b)
}