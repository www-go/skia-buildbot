// Application that captures webpage archives on a CT worker and uploads it to
// Google Storage.
package main

import (
	"flag"
	"fmt"
	"io/ioutil"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"go.skia.org/infra/go/sklog"

	"go.skia.org/infra/ct/go/util"
	"go.skia.org/infra/ct/go/worker_scripts/worker_common"
	"go.skia.org/infra/go/common"
	skutil "go.skia.org/infra/go/util"
)

const (
	// The number of goroutines that will run in parallel to capture archives.
	WORKER_POOL_SIZE = 5
)

var (
	startRange         = flag.Int("start_range", 1, "The number this worker will capture webpage archives from.")
	num                = flag.Int("num", 100, "The total number of archives to capture starting from the start_range.")
	pagesetType        = flag.String("pageset_type", util.PAGESET_TYPE_MOBILE_10k, "The pagesets to use to capture archives. Eg: 10k, Mobile10k, All.")
	chromeCleanerTimer = flag.Duration("cleaner_timer", 30*time.Minute, "How often all chrome processes will be killed on this slave.")
)

func captureArchives() error {
	defer common.LogPanic()
	worker_common.Init()
	if !*worker_common.Local {
		defer util.CleanTmpDir()
	}
	defer util.TimeTrack(time.Now(), "Capturing Archives")
	defer sklog.Flush()

	// Reset the local chromium checkout.
	if err := util.ResetChromiumCheckout(util.ChromiumSrcDir); err != nil {
		return fmt.Errorf("Could not reset %s: %s", util.ChromiumSrcDir, err)
	}
	// Sync the local chromium checkout.
	if err := util.SyncDir(util.ChromiumSrcDir, map[string]string{}, []string{}); err != nil {
		return fmt.Errorf("Could not gclient sync %s: %s", util.ChromiumSrcDir, err)
	}

	// Delete and remake the local webpage archives directory.
	pathToArchives := filepath.Join(util.WebArchivesDir, *pagesetType)
	skutil.RemoveAll(pathToArchives)
	skutil.MkdirAll(pathToArchives, 0700)
	defer skutil.RemoveAll(pathToArchives)

	// Instantiate GcsUtil object.
	gs, err := util.NewGcsUtil(nil)
	if err != nil {
		return err
	}

	// Download pagesets.
	pathToPagesets := filepath.Join(util.PagesetsDir, *pagesetType)
	pagesetsToIndex, err := gs.DownloadSwarmingArtifacts(pathToPagesets, util.PAGESETS_DIR_NAME, *pagesetType, *startRange, *num)
	if err != nil {
		return err
	}
	defer skutil.RemoveAll(pathToPagesets)

	recordWprBinary := filepath.Join(util.TelemetryBinariesDir, util.BINARY_RECORD_WPR)
	timeoutSecs := util.PagesetTypeToInfo[*pagesetType].CaptureArchivesTimeoutSecs
	// Loop through all pagesets.
	fileInfos, err := ioutil.ReadDir(pathToPagesets)
	if err != nil {
		return fmt.Errorf("Unable to read the pagesets dir %s: %s", pathToPagesets, err)
	}

	// Create channel that contains all pageset file names. This channel will
	// be consumed by the worker pool.
	pagesetRequests := util.GetClosedChannelOfPagesets(fileInfos)

	var wg sync.WaitGroup
	// Use a RWMutex for the chromeProcessesCleaner goroutine to communicate to
	// the workers (acting as "readers") when it wants to be the "writer" and
	// kill all zombie chrome processes.
	var mutex sync.RWMutex

	// Loop through workers in the worker pool.
	for i := 0; i < WORKER_POOL_SIZE; i++ {
		// Increment the WaitGroup counter.
		wg.Add(1)

		// Create and run a goroutine closure that captures SKPs.
		go func() {
			// Decrement the WaitGroup counter when the goroutine completes.
			defer wg.Done()

			for pagesetBaseName := range pagesetRequests {
				if filepath.Ext(pagesetBaseName) == ".pyc" {
					// Ignore .pyc files.
					continue
				}

				// Read the pageset.
				pagesetPath := filepath.Join(pathToPagesets, pagesetBaseName)
				decodedPageset, err := util.ReadPageset(pagesetPath)
				if err != nil {
					sklog.Errorf("Could not read %s: %s", pagesetPath, err)
					continue
				}

				sklog.Infof("===== Processing %s =====", pagesetPath)
				index := strconv.Itoa(pagesetsToIndex[path.Join(pathToPagesets, pagesetBaseName)])
				archiveDataFile := addIndexInDataFileLocation(decodedPageset.ArchiveDataFile, index)
				args := []string{
					recordWprBinary,
					util.CAPTURE_ARCHIVES_DEFAULT_CT_BENCHMARK,
					"--browser=reference",
					"--user-agent=" + decodedPageset.UserAgent,
					"--urls-list=" + decodedPageset.UrlsList,
					"--archive-data-file=" + archiveDataFile,
					"--device=desktop",
				}
				env := []string{
					fmt.Sprintf("PYTHONPATH=%s:$PYTHONPATH", pathToPagesets),
					"DISPLAY=:0",
				}

				mutex.RLock()
				// Retry record_wpr binary 3 times if there are any errors.
				retryAttempts := 3
				for i := 0; ; i++ {
					err = util.ExecuteCmd(util.BINARY_PYTHON_2_7_11, args, env, time.Duration(timeoutSecs)*time.Second, nil, nil)
					if err == nil {
						break
					}
					if i >= (retryAttempts - 1) {
						sklog.Errorf("%s failed inspite of 3 retries. Last error: %s", pagesetPath, err)
						break
					}
					time.Sleep(time.Second)
					sklog.Warningf("Retrying due to error: %s", err)
				}
				mutex.RUnlock()
			}
		}()
	}

	if !*worker_common.Local {
		// Start the cleaner.
		go util.ChromeProcessesCleaner(&mutex, *chromeCleanerTimer)
	}

	// Wait for all spawned goroutines to complete.
	wg.Wait()

	// Check to see if there is anything in the pathToArchives dir.
	archivesEmpty, err := skutil.IsDirEmpty(pathToArchives)
	if err != nil {
		return err
	}
	if archivesEmpty {
		return fmt.Errorf("Could not create any archives in %s", pathToArchives)
	}

	// Upload all webpage archives to Google Storage.
	if err := gs.UploadSwarmingArtifacts(util.WEB_ARCHIVES_DIR_NAME, *pagesetType); err != nil {
		return err
	}

	return nil
}

func addIndexInDataFileLocation(originalDataFile string, index string) string {
	fileName := filepath.Base(originalDataFile)
	fileDir := filepath.Dir(originalDataFile)
	return path.Join(fileDir, index, fileName)
}

func main() {
	retCode := 0
	if err := captureArchives(); err != nil {
		sklog.Errorf("Error while capturing archives: %s", err)
		retCode = 255
	}
	os.Exit(retCode)
}
