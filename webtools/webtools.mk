# Provides basic build rules for building minified JS and vulcanized HTML.
#
# Targets
# =======
#   core_js: Builds res/js/core.js.
#
#   elements_html: Builds res/vul/elements.html.
#
#   clean_webtools: Cleans out core.js and elements.html.
#
# Usage
# =====
#  Add the following to your Makefile:
#
#    include ../webtools/webtools.mk
#
#  And define the following:
#
#  BOWER_DIR: The bower directory.
#
#  CORE_SOURCE_FILES: a list of source files that make up core.js.
#     These files should be either present in the project or brought into
#     $(BOWER_DIR) via bower. This makefile runs bower on the local directory
#     to bring in dependencies.

##### core_js ####

# The core_js target builds res/js/core.js from the concatenated source file
# listed in CORE_SOURCE_FILES. The result is minified.
.PHONY: core_js
core_js: node_modules/lastupdate res/js/core.js

# The debug_core_js target does the same thing as core_js, but the file isn't
# minified.
debug_core_js: res/js/core-debug.js
	cp res/js/core-debug.js res/js/core.js

res/js/core.js: res/js/core-debug.js ./node_modules/.bin/uglifyjs
	echo "core.js"
	./node_modules/.bin/uglifyjs res/js/core-debug.js -o res/js/core.js

res/js/core-debug.js: Makefile $(BOWER_DIR)/lastupdate $(CORE_SOURCE_FILES)
	echo "core-debug.js"
	awk 'FNR==1{print ""}{print}' $(CORE_SOURCE_FILES) > res/js/core-debug.js

$(BOWER_DIR)/lastupdate: bower.json ./node_modules/.bin/bower
	./node_modules/.bin/bower update
	ln --force --symbolic ../../$(BOWER_DIR) res/imp/bower_components
	touch $(BOWER_DIR)/lastupdate

#### elements_html ####

# The elements_html target builds a vulcanized res/vul/elements.html from
# elements.html.
elements_html: res/vul/elements.html

# The debug_elements_html target just copies elements.html into res/vul/elements.html.
debug_elements_html:
	-mkdir res/vul
	cp elements.html res/vul/elements.html
	ln --force --symbolic ../../$(BOWER_DIR) res/imp/bower_components

res/vul/elements.html: res/imp/*.html elements.html ./node_modules/.bin/vulcanize
	./node_modules/.bin/vulcanize --csp=false --inline=true --strip=false --abspath=./ elements.html -o res/vul/elements.html

#### clean_webtools ####

clean_webtools:
	-rm res/vul/elements.html
	-rm res/js/core.js
	-rm res/js/core-debug.js

#### Rules to npm install needed tools ####

./node_modules/.bin/vulcanize:
	npm install vulcanize --save-dev

./node_modules/.bin/bower:
	npm install bower --save-dev

./node_modules/.bin/uglifyjs:
	npm install uglify-js --save-dev

#### npm install dependencies ####

node_modules/lastupdate: package.json
	npm install
	touch node_modules/lastupdate

