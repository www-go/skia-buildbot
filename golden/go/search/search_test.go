package search

import (
	"fmt"
	"net/url"
	"os"
	"testing"
	"time"

	assert "github.com/stretchr/testify/require"

	"go.skia.org/infra/go/eventbus"
	"go.skia.org/infra/go/gs"
	"go.skia.org/infra/go/testutils"
	"go.skia.org/infra/go/util"
	"go.skia.org/infra/golden/go/expstorage"
	"go.skia.org/infra/golden/go/indexer"
	"go.skia.org/infra/golden/go/mocks"
	"go.skia.org/infra/golden/go/serialize"
	"go.skia.org/infra/golden/go/storage"
	"go.skia.org/infra/golden/go/types"
)

const (
	// Directory with testdata.
	TEST_DATA_DIR = "./testdata"

	// Local file location of the test data.
	TEST_DATA_PATH = TEST_DATA_DIR + "/10-test-sample.tile"

	// Folder in the testdata bucket. See go/testutils for details.
	TEST_DATA_STORAGE_PATH = "gold-testdata/10-test-sample.tile"

	// REPO_URL is the url of the repo to check out.
	REPO_URL = "https://skia.googlesource.com/skia"

	// REPO_DIR contains the location of where to check out Skia for benchmarks.
	REPO_DIR = "./skia"

	// N_COMMITS is the number of commits used in benchmarks.
	N_COMMITS = 50

	// Database user used by benchmarks.
	DB_USER = "readwrite"
)

func TestCompareTests(t *testing.T) {
	testutils.SkipIfShort(t)

	err := gs.DownloadTestDataFile(t, gs.TEST_DATA_BUCKET, TEST_DATA_STORAGE_PATH, TEST_DATA_PATH)
	assert.NoError(t, err, "Unable to download testdata.")
	defer testutils.RemoveAll(t, TEST_DATA_DIR)

	sample := loadSample(t, TEST_DATA_PATH)

	tileBuilder := mocks.NewMockTileBuilderFromTile(t, sample.Tile)
	eventBus := eventbus.New(nil)
	expStore := expstorage.NewMemExpectationsStore(eventBus)
	err = expStore.AddChange(sample.Expectations.Tests, "testuser")
	assert.NoError(t, err)

	storages := &storage.Storage{
		ExpectationsStore: expStore,
		MasterTileBuilder: tileBuilder,
		DigestStore: &mocks.MockDigestStore{
			FirstSeen: time.Now().Unix(),
			OkValue:   true,
		},
		DiffStore: mocks.NewMockDiffStore(),
		EventBus:  eventBus,
	}

	ixr, err := indexer.New(storages, time.Minute)
	assert.NoError(t, err)
	tile := ixr.GetIndex().GetTile(false)

	// testNameSet collects all test names and the set of digests for
	// each test to establish a ground truth for the search below.
	testNameSet := map[string]util.StringSet{}
	for _, trace := range tile.Traces {
		testName := trace.Params()[types.PRIMARY_KEY_FIELD]
		if _, ok := testNameSet[testName]; !ok {
			testNameSet[testName] = util.StringSet{}
		}
		testNameSet[testName].AddLists(trace.(*types.GoldenTrace).Values)
	}

	const MAX_DIM = 5
	for testName, digestSet := range testNameSet {
		delete(digestSet, types.MISSING_DIGEST)

		// limit is the expected number of results.
		limit := util.MinInt(len(digestSet), MAX_DIM)

		// rowLimit is the number of elements expected in each row.
		rowLimit := util.MinInt(len(digestSet)-1, MAX_DIM)
		util.MaxInt(len(digestSet)-1, limit)

		// Make sure the query searches for the current testName. It is assumed
		// that all tests in the tile have source_type == 'gm'.
		q, err := url.ParseQuery(fmt.Sprintf("source_type=gm&name=%s", testName))
		assert.NoError(t, err)
		ctQuery := &CTQuery{
			Test: testName,
			RowQuery: &Query{
				Pos:            true,
				Neg:            true,
				Unt:            true,
				Head:           true,
				IncludeIgnores: false,
				Query:          q,
				Limit:          limit,
			},
			ColumnQuery: &Query{
				Pos:            true,
				Neg:            true,
				Unt:            true,
				Head:           true,
				IncludeIgnores: false,
				Query:          q,
				Limit:          limit,
			},
			Match:       []string{},
			SortRows:    "n",
			SortColumns: "diff",
			RowsDir:     "desc",
			ColumnsDir:  "asc",
		}
		ret, err := CompareTest(ctQuery, storages, ixr.GetIndex())
		assert.NoError(t, err)

		// Make sure the rows are as expected.
		assert.Equal(t, limit, len(ret.Grid.Cells))
		for idx, row := range ret.Grid.Cells {
			foundDigestSet := util.StringSet{}
			for _, cell := range row {
				foundDigestSet[cell.Digest] = true
			}
			// Make sure there are not duplicate digest in a row.
			assert.Equal(t, len(row), len(foundDigestSet))
			// Make sure the 'row' digest is not in the row, i.e. compared to itself.
			assert.False(t, foundDigestSet[ret.Grid.Rows[idx].Digest])
			// Make sure we get the expected number of digests in this row.
			assert.Equal(t, rowLimit, len(row))

			// Make sure the found digests are fully contained in the whole set.
			assert.Equal(t, digestSet.Intersect(foundDigestSet), foundDigestSet)
		}
		assert.Equal(t, limit, len(ret.Grid.Rows))
	}
}

func loadSample(t assert.TestingT, fileName string) *serialize.Sample {
	file, err := os.Open(fileName)
	assert.NoError(t, err)

	sample, err := serialize.DeserializeSample(file)
	assert.NoError(t, err)

	return sample
}