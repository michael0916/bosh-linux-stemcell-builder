package smoke_test

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"strconv"

	yaml "gopkg.in/yaml.v2"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

func fillUpFileWithRandomCharacters(filepath string) {
	command := fmt.Sprintf(`sudo bash -c "dd if=<(tr -cd '[:alnum:]' < /dev/urandom) count=10000 bs=1024 >> %s"`, filepath)
	stdOut, stdErr, exitStatus, err := bosh.Run(
		"ssh",
		"default/0",
		command,
	)
	Expect(err).ToNot(HaveOccurred())
	Expect(exitStatus).To(Equal(0), fmt.Sprintf("stdOut: %s \n stdErr: %s", stdOut, stdErr))
}

var _ = Describe("Stemcell", func() {
	Context("when logrotate wtmp/btmp logs", func() {
		It("should rotate the wtmp/btmp logs", func() {
			stdOut, stdErr, exitStatus, err := bosh.Run("ssh", "default/0", `sudo sed -E -i "s/[0-9,]+/\*/" /etc/cron.d/logrotate`)
			Expect(err).ToNot(HaveOccurred())
			Expect(exitStatus).To(Equal(0), fmt.Sprintf("stdOut: %s \n stdErr: %s", stdOut, stdErr))

			fillUpFileWithRandomCharacters("/var/log/wtmp")
			Eventually("/var/log/wtmp", 2*time.Minute, 15*time.Second).Should(BeLogRotated())

			fillUpFileWithRandomCharacters("/var/log/btmp")
			Eventually("/var/log/btmp", 2*time.Minute, 15*time.Second).Should(BeLogRotated())
		})
	})

	Context("when syslog threshold limit is reached", func() {
		It("should rotate the logs", func() {
			_, _, exitStatus, err := bosh.Run("ssh", "default/0", `sudo sed -E -i "s/[0-9,]+/\*/" /etc/cron.d/logrotate`)
			Expect(err).ToNot(HaveOccurred())
			Expect(exitStatus).To(Equal(0))

			_, _, exitStatus, err = bosh.Run("ssh", "default/0", `logger "old syslog content"`)
			Expect(err).ToNot(HaveOccurred())
			Expect(exitStatus).To(Equal(0))

			fillUpFileWithRandomCharacters("/var/log/syslog")
			Eventually("/var/vcap/data/root_log/syslog", 2*time.Minute, 15*time.Second).Should(BeLogRotated())

			stdOut, stdErr, exitStatus, err := bosh.Run("ssh", "default/0", `logger "new syslog content"`)
			Expect(err).ToNot(HaveOccurred())
			Expect(exitStatus).To(Equal(0), fmt.Sprintf("Could not log using logger on the syslog forwarder! \n stdOut: %s \n stdErr: %s", stdOut, stdErr))

			stdOut, stdErr, exitStatus, err = bosh.Run(
				"ssh",
				"default/0",
				`sudo grep "[n]ew syslog content\|[o]ld syslog content" /var/vcap/data/root_log/syslog `,
			)
			Expect(err).ToNot(HaveOccurred())
			Expect(exitStatus).To(Equal(0), fmt.Sprintf("Could not read from syslog stdOut: %s \n stdErr: %s", stdOut, stdErr))
			Expect(stdOut).To(ContainSubstring("new syslog content"))
			Expect(stdOut).NotTo(ContainSubstring("old syslog content"))
		})
	})

	It("#134136191: auth.log should not contain 'No such file or directory' errors", func() {
		tempFile, err := ioutil.TempFile(os.TempDir(), "auth.log")
		Expect(err).ToNot(HaveOccurred())
		authLogAbsPath, err := filepath.Abs(tempFile.Name())
		Expect(err).ToNot(HaveOccurred())

		stdOut, stdErr, exitStatus, err := bosh.Run("ssh", "default/0", `sudo cp /var/log/auth.log /tmp/ && sudo chmod 777 /tmp/auth.log`)
		Expect(err).ToNot(HaveOccurred())
		Expect(exitStatus).To(Equal(0), fmt.Sprintf("Could not create nested log path! \n stdOut: %s \n stdErr: %s", stdOut, stdErr))

		_, _, exitStatus, err = bosh.Run("scp", "default/0:/tmp/auth.log", authLogAbsPath)
		Expect(err).ToNot(HaveOccurred())
		Expect(exitStatus).To(Equal(0))

		contents, err := ioutil.ReadAll(tempFile)
		Expect(err).ToNot(HaveOccurred())
		Expect(contents).ToNot(ContainSubstring("No such file or directory"))
	})

	It("#141987897: disables ipv6 in the kernel", func() {
		stdOut, _, exitStatus, err := bosh.Run("--column=stdout", "ssh", "default/0", "-r", "-c", `sudo netstat -lnp | grep sshd | awk '{ print $4 }'`)
		Expect(err).ToNot(HaveOccurred())
		Expect(exitStatus).To(Equal(0))
		Expect(strings.Split(strings.TrimSpace(stdOut), "\n")).To(Equal([]string{"0.0.0.0:22"}))
	})

	It("#140456537: enables sysstat", func() {
		_, _, exitStatus, err := bosh.Run(
			"--column=stdout",
			"ssh", "default/0", "-r", "-c",
			// sleep to ensure we have multiple samples so average can be verified
			`sudo /usr/lib/sysstat/debian-sa1 && sudo /usr/lib/sysstat/debian-sa1 1 1 && sleep 2 && sudo /usr/lib/sysstat/debian-sa1 1 1`,
		)
		Expect(err).ToNot(HaveOccurred())
		Expect(exitStatus).To(Equal(0))

		stdOut, _, exitStatus, err := bosh.Run(
			"--column=stdout",
			"ssh", "default/0", "-r", "-c",
			`sudo sar`,
		)
		Expect(err).ToNot(HaveOccurred())
		Expect(exitStatus).To(Equal(0))
		Expect(stdOut).To(MatchRegexp(`^Linux`))
		Expect(stdOut).To(MatchRegexp(`\nAverage:\s+`))
	})

	It("#146390925: rsyslog logs with precision timestamps", func() {
		stdout, _, exitStatus, err := bosh.Run(
			"--column=stdout",
			"ssh", "default/0", "-r",
			"-c", `logger story146390925 && sleep 1 && sudo grep story146390925 /var/log/syslog`,
		)
		Expect(err).ToNot(HaveOccurred())
		Expect(exitStatus).To(Equal(0))

		Expect(stdout).To(MatchRegexp(`\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}.\d{1,6}\+00:00 [\w-]+ bosh_[^ ]+: story146390925`))
	})

	It("#153023582: network interface eth0 exists", func() {
		stdout, _, exitStatus, err := bosh.Run(
			"--column=stdout",
			"ssh", "default/0", "-r", "-c",
			`sudo ip addr show dev eth0`,
		)
		Expect(err).ToNot(HaveOccurred())
		Expect(exitStatus).To(Equal(0))
		Expect(stdout).To(ContainSubstring("eth0"))
	})

	Context("when synchronizing the clock on the instance via ntp", func() {
		It("corrects xenial systemtime via chrony", func() {
			if os.Getenv("BOSH_os_name") != "ubuntu-xenial" {
				Skip(`please set BOSH_os_name to "ubuntu-xenial" run this test`)
			}

			stdout, _, exitStatus, err := bosh.Run(
				"--column=stdout",
				"ssh", "default/0", "-r", "-c",
				`sudo chronyc -a tracking`,
			)
			Expect(err).ToNot(HaveOccurred())
			Expect(exitStatus).To(Equal(0))

			ntpServer := regexp.MustCompile(`Reference ID\s+:(\s[0-9]{1,3}\.[0-9]{1,3}\.[0-9]{1,3}\.[0-9]{1,3})`)
			match := ntpServer.FindAllStringSubmatch(stdout, -1)
			Expect(match[0][1]).NotTo(Equal("0.0.0.0"))

			systemTime := regexp.MustCompile(`System time\s+:\s(\d\.\d+)`)
			match = systemTime.FindAllStringSubmatch(stdout, -1)

			drift, err := strconv.ParseFloat(match[0][1], 32)
			Expect(err).NotTo(HaveOccurred())

			Expect(drift).To(BeNumerically("<", 1))

			By("running the sync-time script, we do not see an error", func() {
				_, _, exitStatus, err := bosh.Run(
					"--column=stdout",
					"ssh", "default/0", "-r", "-c",
					`sudo /var/vcap/bosh/bin/sync-time`,
				)
				Expect(err).ToNot(HaveOccurred())
				Expect(exitStatus).To(Equal(0))
			})
		})
	})

	It("#153391129: removes dev tools and static libraries", func() {
		opsFilePath, err := filepath.Abs("remove_dev_tools_and_static_libraries.yml")
		Expect(err).NotTo(HaveOccurred())

		bosh.SafeDeploy("--recreate", "-o", opsFilePath)
		stdout, _, exitStatus, err := bosh.Run(
			"--column=stdout",
			"ssh", "default/0", "-r", "-c",
			`sudo cat /var/vcap/bosh/etc/dev_tools_file_list | xargs -n1 -I {} /bin/bash -c '[ ! -e % ] || echo found file %'`,
		)
		Expect(err).ToNot(HaveOccurred())
		Expect(exitStatus).To(Equal(0))
		Expect(strings.TrimSpace(stdout)).To(Equal("-"))

		stdout, _, exitStatus, err = bosh.Run(
			"--column=stdout",
			"ssh", "default/0", "-r", "-c",
			`sudo cat /var/vcap/bosh/etc/static_libraries_list | xargs -n1 -I % /bin/bash -c '[ ! -e % ] || echo found library %'`,
		)
		Expect(err).ToNot(HaveOccurred())
		Expect(exitStatus).To(Equal(0))
		Expect(strings.TrimSpace(stdout)).To(Equal("-"))
	})

	Context("#153887510: basic bind mount locations are verified", func() {
		It("contains all the expected files in /var/log", func() {
			_, _, exitStatus, err := bosh.Run(
				"--column=stdout",
				"ssh", "default/0", "-r", "-c",
				`logger -p daemon.error "Line in daemon.log"; sudo ls /var/log/{audit,auth.log,btmp,daemon.log,kern.log,lastlog,syslog,sysstat,wtmp}`,
			)

			Expect(err).ToNot(HaveOccurred())
			Expect(exitStatus).To(Equal(0))
		})

		It("/var/log is bind mounted to a /dev/disk[/root_log]", func() {
			stdout, _, exitStatus, err := bosh.Run(
				"--column=stdout",
				"ssh", "default/0", "-r", "-c",
				`sudo findmnt -n -T /var/log | awk '{print $1 " " $2}'`,
			)

			Expect(err).ToNot(HaveOccurred())
			Expect(exitStatus).To(Equal(0))
			Expect(stdout).To(MatchRegexp(`\/var\/log\s+\/dev\/[a-z0-9]+\[\/root_log\]`))
		})

		It("/tmp is bind mounted to a /dev/disk[/root_tmp]", func() {
			stdout, _, exitStatus, err := bosh.Run(
				"--column=stdout",
				"ssh", "default/0", "-r", "-c",
				`sudo findmnt -n -T /tmp | awk '{print $1 " " $2}'`,
			)

			Expect(err).ToNot(HaveOccurred())
			Expect(exitStatus).To(Equal(0))
			Expect(stdout).To(MatchRegexp(`\/tmp\s+\/dev\/[a-z0-9]+\[\/root_tmp\]`))
		})

		It("/var/tmp is bind mounted to a /dev/disk[/root_tmp]", func() {
			stdout, _, exitStatus, err := bosh.Run(
				"--column=stdout",
				"ssh", "default/0", "-r", "-c",
				`sudo findmnt -n -T /var/tmp | awk '{print $1 " " $2}'`,
			)

			Expect(err).ToNot(HaveOccurred())
			Expect(exitStatus).To(Equal(0))
			Expect(stdout).To(MatchRegexp(`\/tmp\s+\/dev\/[a-z0-9]+\[\/root_tmp\]`))
		})

		It("can write a file to the bind mount and appear in the device source", func() {
			_, _, exitStatus, err := bosh.Run(
				"--column=stdout",
				"ssh", "default/0", "-r", "-c",
				`sudo touch /var/{log/1,vcap/data/root_log/2}; sudo ls /var/{log,vcap/data/root_log}/{1,2}`,
			)

			Expect(err).ToNot(HaveOccurred())
			Expect(exitStatus).To(Equal(0))
		})

		It("can write log messages to the system log file", func() {
			_, _, exitStatus, err := bosh.Run(
				"--column=stdout",
				"ssh", "default/0", "-r", "-c",
				`sudo adduser --disabled-password --gecos "" --quiet testuser && sudo -u testuser logger syslog-line && sudo userdel testuser`,
			)

			Expect(err).ToNot(HaveOccurred())
			Expect(exitStatus).To(Equal(0))
		})
	})

	Context("#164749230, when using targeted blobstores", func() {
		type agentSettings struct {
			Env struct {
				BoshEnv struct {
					Blobstores []struct {
						Options struct {
							Endpoint string `json:"endpoint"`
							Password string `json:"password"`
							Tls      struct {
								Cert struct {
									Ca string `json:"ca"`
								} `json:"cert"`
							} `json:"tls"`
						} `json:"options"`
					} `json:"blobstores"`
				} `json:"bosh"`
			} `json:"env"`
		}

		type blobStoreVars struct {
			Endpoint               string `yaml:"endpoint"`
			BlobstoreAgentPassword string `yaml:"blobstore_agent_password"`
			BlobstoreCaCertificate string `yaml:"blobstore_ca_certificate"`
		}

		AfterEach(func() {
			bosh.SafeDeploy()
		})

		Context("when deploying with a invalid logs blobstore", func() {
			It("should fail to get logs, but the deploy should succeed", func() {
				config := agentSettings{}
				stdout, _, exitStatus, err := bosh.Run(
					"--column=stdout",
					"ssh", "default/0", "-r", "-c",
					`sudo cat /var/vcap/bosh/settings.json`,
				)
				Expect(err).ToNot(HaveOccurred())
				Expect(exitStatus).To(Equal(0))

				err = json.Unmarshal([]byte(stdout), &config)
				Expect(err).NotTo(HaveOccurred())

				opsFile, err := filepath.Abs("add-invalid-logs-blobstore.yml")
				Expect(err).ToNot(HaveOccurred())

				blobstoreVarFile, err := ioutil.TempFile("", "blobstore_config")
				Expect(err).NotTo(HaveOccurred())
				bsv := blobStoreVars{
					Endpoint:               config.Env.BoshEnv.Blobstores[0].Options.Endpoint,
					BlobstoreAgentPassword: config.Env.BoshEnv.Blobstores[0].Options.Password,
					BlobstoreCaCertificate: config.Env.BoshEnv.Blobstores[0].Options.Tls.Cert.Ca,
				}
				yamlBytes, err := yaml.Marshal(bsv)
				Expect(err).NotTo(HaveOccurred())
				blobstoreVarFile.Write(yamlBytes)
				blobstoreVarFile.Close()

				bosh.SafeDeploy("-o", opsFile,
					"--vars-file", blobstoreVarFile.Name(),
				)

				name, err := ioutil.TempDir("", "bosh-logs")
				Expect(err).ToNot(HaveOccurred())

				stdout, _, exitStatus, err = bosh.Run(
					"logs", "default/0", fmt.Sprintf("--dir=%s", name),
				)
				Expect(err).To(HaveOccurred())
				Expect(exitStatus).To(Equal(1))
				Expect(strings.TrimSpace(stdout)).To(MatchRegexp("bosh-blobstore-dav -c /var/vcap/bosh/etc/invalid/blobstore-dav.json"))
			})
		})

		Context("when deploying with an invalid packages blobstore", func() {
			It("should fail to deploy", func() {
				config := agentSettings{}
				stdout, _, exitStatus, err := bosh.Run(
					"--column=stdout",
					"ssh", "default/0", "-r", "-c",
					`sudo cat /var/vcap/bosh/settings.json`,
				)
				Expect(err).ToNot(HaveOccurred())
				Expect(exitStatus).To(Equal(0))

				err = json.Unmarshal([]byte(stdout), &config)
				Expect(err).NotTo(HaveOccurred())

				opsFile, err := filepath.Abs("add-invalid-packages-blobstore.yml")
				Expect(err).ToNot(HaveOccurred())

				blobstoreVarFile, err := ioutil.TempFile("", "blobstore_config")
				Expect(err).NotTo(HaveOccurred())
				bsv := blobStoreVars{
					Endpoint:               config.Env.BoshEnv.Blobstores[0].Options.Endpoint,
					BlobstoreAgentPassword: config.Env.BoshEnv.Blobstores[0].Options.Password,
					BlobstoreCaCertificate: config.Env.BoshEnv.Blobstores[0].Options.Tls.Cert.Ca,
				}
				yamlBytes, err := yaml.Marshal(bsv)
				Expect(err).NotTo(HaveOccurred())
				blobstoreVarFile.Write(yamlBytes)
				blobstoreVarFile.Close()

				stdOut, stdErr, exitStatus, err := bosh.UnsafeDeploy("-o", opsFile,
					"--vars-file", blobstoreVarFile.Name(),
				)
				Expect(err).To(HaveOccurred())
				Expect(stdOut).To(MatchRegexp("bosh-blobstore-dav -c /var/vcap/bosh/etc/invalid/blobstore-dav.json"))
				Expect(exitStatus).To(Equal(1), fmt.Sprintf("stdOut: %s \n stdErr: %s", stdOut, stdErr))
			})
		})
	})
})
