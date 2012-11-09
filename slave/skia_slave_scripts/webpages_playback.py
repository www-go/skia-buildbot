#!/usr/bin/env python
# Copyright (c) 2012 The Chromium Authors. All rights reserved.
# Use of this source code is governed by a BSD-style license that can be
# found in the LICENSE file.

"""Archives or replays webpages and creates skps in a Google Storage location.

To archive webpages and store skp files (will be run rarely):

cd ../buildbot/slave/skia_slave_scripts
PYTHONPATH=../../third_party/chromium_trunk/tools/perf:\
../../third_party/chromium_buildbot/scripts:\
../../third_party/chromium_buildbot/site_config \
python webpages_playback.py --dest_gsbase=gs://rmistry \
--record=True


To replay archived webpages and re-generate skp files (will be run whenever
SkPicture.PICTURE_VERSION changes):

cd ../buildbot/slave/skia_slave_scripts
PYTHONPATH=../../third_party/chromium_trunk/tools/perf:\
../../third_party/chromium_buildbot/scripts:\
../../third_party/chromium_buildbot/site_config \
python webpages_playback.py --dest_gsbase=gs://rmistry

"""

import optparse
import os
import posixpath
import shutil
import sys
import tempfile

from perf_tools import multipage_benchmark_runner
from slave import slave_utils
from utils import file_utils
from utils import gs_utils

from build_step import PLAYBACK_CANNED_ACL
from playback_dirs import ROOT_PLAYBACK_DIR_NAME
from playback_dirs import SKPICTURES_DIR_NAME


# Local archive and skp directories.
LOCAL_PLAYBACK_ROOT_DIR = os.path.join(
    tempfile.gettempdir(), ROOT_PLAYBACK_DIR_NAME)
LOCAL_REPLAY_WEBPAGES_ARCHIVE_DIR = os.path.join(
    os.path.abspath(os.path.dirname(__file__)), 'page_sets', 'data')
LOCAL_RECORD_WEBPAGES_ARCHIVE_DIR = os.path.join(
    tempfile.gettempdir(), ROOT_PLAYBACK_DIR_NAME, 'webpages_archive')
LOCAL_SKP_DIR = os.path.join(
    tempfile.gettempdir(), ROOT_PLAYBACK_DIR_NAME, SKPICTURES_DIR_NAME)


class SkPicturePlayback(object):
  """Class that archives or replays webpages and creates skps."""

  def __init__(self, parse_options):
    """Constructs a SkPicturePlayback BuildStep instance."""
    self._page_set = parse_options.page_set
    self._dest_gsbase = parse_options.dest_gsbase
    self._record = parse_options.record
    self._wpr_file_name = self._page_set.split('/')[-1].split('.')[0] + '.wpr'

  def Run(self):
    """Run the SkPicturePlayback BuildStep."""
    # Delete the local root directory if it already exists.
    if os.path.exists(LOCAL_PLAYBACK_ROOT_DIR):
      shutil.rmtree(LOCAL_PLAYBACK_ROOT_DIR)

    # Create the required local storage directories.
    self._CreateLocalStorageDirs()

    if not self._record:
      # Get the webpages archive from Google Storage so that it can be replayed.
      self._DownloadArchiveFromStorage()

    # Clear all command line arguments and add only the ones supported by
    # the skpicture_printer benchmark.
    self._SetupArgsForSkPrinter()

    # Run the skpicture_printer script which:
    # Creates an archive of the specified webpages if '--record' is specified.
    # Saves all webpages in the page_set as skp files.
    multipage_benchmark_runner.Main()

    if self._record:
      # Move over the created archive into the local webpages archive directory.
      shutil.move(
          os.path.join(LOCAL_REPLAY_WEBPAGES_ARCHIVE_DIR, self._wpr_file_name),
          LOCAL_RECORD_WEBPAGES_ARCHIVE_DIR)

    # Rename generated skp files into more descriptive names.
    self._RenameSkpFiles()

    # Delete the local wpr now that we are done with it.
    shutil.rmtree(LOCAL_REPLAY_WEBPAGES_ARCHIVE_DIR)

    # Delete the skp directory on Google Storage since it will be replaced.
    gs_utils.DeleteStorageObject(
        posixpath.join(self._dest_gsbase, ROOT_PLAYBACK_DIR_NAME,
        SKPICTURES_DIR_NAME))

    # Copy the directory structure in the root directory into Google Storage.
    gs_status = slave_utils.GSUtilCopyDir(
        src_dir=LOCAL_PLAYBACK_ROOT_DIR, gs_base=self._dest_gsbase,
        dest_dir=ROOT_PLAYBACK_DIR_NAME, gs_acl=PLAYBACK_CANNED_ACL)
    if gs_status != 0:
      raise Exception(
          'ERROR: GSUtilCopyDir error %d. "%s" -> "%s/%s"' % (
              gs_status, LOCAL_PLAYBACK_ROOT_DIR, self._dest_gsbase,
              ROOT_PLAYBACK_DIR_NAME))
    return 0

  def _RenameSkpFiles(self):
    """Rename generated skp files into more descriptive names.

    All skp files are currently called layer_X.skp where X is an integer, they
    will be renamed into http_website_name_X.skp.

    Eg: http_news_yahoo_com/layer_0.skp -> http_news_yahoo_com_0.skp
    """
    for (dirpath, unused_dirnames, filenames) in os.walk(LOCAL_SKP_DIR):
      if not dirpath or not filenames:
        continue
      basename = os.path.basename(dirpath)
      for filename in filenames:
        filename_parts = filename.split('.')
        extension = filename_parts[1]
        integer = filename_parts[0].split('_')[1]
        new_filename = '%s_%s.%s' % (basename, integer, extension)
        shutil.move(os.path.join(dirpath, filename),
                    os.path.join(LOCAL_SKP_DIR, new_filename))
      shutil.rmtree(dirpath)

  def _SetupArgsForSkPrinter(self):
    """Setup arguments for the skpicture_printer script.

    Clears all command line arguments and adds only the ones supported by
    skpicture_printer.
    """
    # Clear all command line arguments.
    del sys.argv[:]
    # Dummy first argument.
    sys.argv.append('dummy_file_name')
    if self._record:
      # Create a new wpr file.
      sys.argv.append('--record')
    # Use the system browser.
    sys.argv.append('--browser=system')
    # Output skp files to skpictures_dir.
    sys.argv.append('--outdir=' + LOCAL_SKP_DIR)
    # Point to the skpicture_printer benchmark.
    sys.argv.append('skpicture_printer')
    # Point to the top 25 webpages page set.
    sys.argv.append(self._page_set)

  def _CreateLocalStorageDirs(self):
    """Creates required local storage directories for this script."""
    file_utils.CreateCleanLocalDir(LOCAL_REPLAY_WEBPAGES_ARCHIVE_DIR)
    file_utils.CreateCleanLocalDir(LOCAL_RECORD_WEBPAGES_ARCHIVE_DIR)
    file_utils.CreateCleanLocalDir(LOCAL_SKP_DIR)

  def _DownloadArchiveFromStorage(self):
    """Download the webpages archive from Google Storage."""
    wpr_source = posixpath.join(
        self._dest_gsbase, ROOT_PLAYBACK_DIR_NAME, 'webpages_archive',
        self._wpr_file_name)
    slave_utils.GSUtilDownloadFile(
        src=wpr_source, dst=LOCAL_REPLAY_WEBPAGES_ARCHIVE_DIR)


if '__main__' == __name__:
  option_parser = optparse.OptionParser()
  option_parser.add_option(
      '', '--page_set',
      help='Specifies the page set to use to archive.',
      default=(
          os.path.join(os.path.abspath(os.path.dirname(__file__)),
                       'page_sets', 'skia_set.json')))
  option_parser.add_option(
      '', '--record',
      help='Specifies whether a new website archive should be created.',
      default=False)
  option_parser.add_option(
      '', '--dest_gsbase',
      help='gs:// bucket_name, the bucket to upload the file to')
  options, unused_args = option_parser.parse_args()

  playback = SkPicturePlayback(options)
  sys.exit(playback.Run())
