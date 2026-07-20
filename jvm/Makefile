# Determine the architecture
ARCH := $(shell uname -m | sed 's/x86_64/amd64/;s/aarch64/arm64/')

# Compiler and tool configuration
DETECTED_JAVA_HOME := $(shell command -v javac >/dev/null 2>&1 && dirname "$$(dirname "$$(readlink -f "$$(command -v javac)")")")
JAVA_HOME ?= $(if $(DETECTED_JAVA_HOME),$(DETECTED_JAVA_HOME),/usr/lib/jvm/java-17-openjdk-$(ARCH))
JAVAC = $(JAVA_HOME)/bin/javac
JAR = $(JAVA_HOME)/bin/jar
CC = gcc
CFLAGS = -Wall -Werror -fPIC -shared -I$(JAVA_HOME)/include -I$(JAVA_HOME)/include/linux

# Directory structure
BUILD_DIR = dist
LOADER_BUILD_DIR = $(BUILD_DIR)/java/loader
QTAP_BUILD_DIR = $(BUILD_DIR)/java/qtap
JAVASSL_BUILD_DIR = $(BUILD_DIR)/java/javassl
C_BUILD_DIR = $(BUILD_DIR)/c

# Java configuration
JAVA_PACKAGE_PATH = io/qpoint/qtap

# Loader
LOADER_SRC = java/LoadAgent.java
LOADER_MANIFEST = java/resources/loader-manifest.mf
LOADER_JAR = $(BUILD_DIR)/qtap-loader.jar

# Qtap
QTAP_SRC = java/QtapAgent.java
QTAP_MANIFEST = java/resources/qtap-manifest.mf
QTAP_JAR = $(BUILD_DIR)/qtap.jar

# JavaSsl
JAVASSL_SRC = java/JavaSsl.java
JAVASSL_MANIFEST = java/resources/javassl-manifest.mf
JAVASSL_JAR = $(BUILD_DIR)/java-ssl.jar

# C configuration
C_SOURCES = c/libqtap.c
C_OBJECTS = $(C_SOURCES:c/%.c=$(C_BUILD_DIR)/%.o)
C_TARGET = $(BUILD_DIR)/libqtap.so

# ASM dependencies
ASM_VERSION = 9.5
ASM_JAR = asm-$(ASM_VERSION).jar
ASM_COMMONS_JAR = asm-commons-$(ASM_VERSION).jar
ASM_UTIL_JAR = asm-util-$(ASM_VERSION).jar
ASM_URL = https://repo1.maven.org/maven2/org/ow2/asm/asm/$(ASM_VERSION)/$(ASM_JAR)
ASM_COMMONS_URL = https://repo1.maven.org/maven2/org/ow2/asm/asm-commons/$(ASM_VERSION)/$(ASM_COMMONS_JAR)
ASM_UTIL_URL = https://repo1.maven.org/maven2/org/ow2/asm/asm-util/$(ASM_VERSION)/$(ASM_UTIL_JAR)

# Google Java Format configuration
GJF_VERSION = 1.19.2
GJF_JAR = $(BUILD_DIR)/google-java-format-$(GJF_VERSION)-all-deps.jar
GJF_URL = https://github.com/google/google-java-format/releases/download/v$(GJF_VERSION)/google-java-format-$(GJF_VERSION)-all-deps.jar
GJF_JAVA_FILES = java/*.java

# JVM flags for google-java-format on Java 17+
GJF_JVM_FLAGS = --add-exports=jdk.compiler/com.sun.tools.javac.api=ALL-UNNAMED \
                --add-exports=jdk.compiler/com.sun.tools.javac.code=ALL-UNNAMED \
                --add-exports=jdk.compiler/com.sun.tools.javac.file=ALL-UNNAMED \
                --add-exports=jdk.compiler/com.sun.tools.javac.parser=ALL-UNNAMED \
                --add-exports=jdk.compiler/com.sun.tools.javac.tree=ALL-UNNAMED \
                --add-exports=jdk.compiler/com.sun.tools.javac.util=ALL-UNNAMED

# Custom JRE configuration
JRE_VERSION = 17.0.9+9
JRE_VERSION_UNDERSCORE = $(subst +,_,$(JRE_VERSION))
JRE_BASE_URL = https://github.com/adoptium/temurin17-binaries/releases/download/jdk-$(JRE_VERSION)
JRE_BUNDLE = $(BUILD_DIR)/custom-jre.tar.gz

.PHONY: all check-java clean java c jre

all: java c

check-java:
	@if [ ! -x "$(JAVAC)" ]; then \
		echo 'JDK 17 is required; install openjdk-17-jdk-headless or set JAVA_HOME.' >&2; \
		exit 1; \
	fi

dirs:
	@mkdir -p $(BUILD_DIR) $(LOADER_BUILD_DIR) $(QTAP_BUILD_DIR) $(JAVASSL_BUILD_DIR) $(C_BUILD_DIR)

# Java targets
java: $(LOADER_JAR) $(QTAP_JAR) $(JAVASSL_JAR) $(JRE_BUNDLE)

# Loader JAR - just needs the LoadAgent class
$(LOADER_JAR): $(LOADER_SRC) $(LOADER_MANIFEST) | dirs
	@mkdir -p $(LOADER_BUILD_DIR)
	$(JAVAC) -d $(LOADER_BUILD_DIR) $(LOADER_SRC)
	$(JAR) cmf $(LOADER_MANIFEST) $@ -C $(LOADER_BUILD_DIR) $(JAVA_PACKAGE_PATH)/LoadAgent.class

# JavaSsl JAR - just needs the JavaSsl class
$(JAVASSL_JAR): $(JAVASSL_SRC) $(JAVASSL_MANIFEST) | dirs
	@mkdir -p $(JAVASSL_BUILD_DIR)
	$(JAVAC) --release 8 -d $(JAVASSL_BUILD_DIR) $(JAVASSL_SRC)
	$(JAR) cmf $(JAVASSL_MANIFEST) $@ -C $(JAVASSL_BUILD_DIR) $(JAVA_PACKAGE_PATH)/JavaSsl.class

# Qtap JAR - needs all classes and ASM dependencies
$(QTAP_JAR): $(QTAP_SRC) $(QTAP_MANIFEST) $(BUILD_DIR)/$(ASM_JAR) $(BUILD_DIR)/$(ASM_COMMONS_JAR) $(BUILD_DIR)/$(ASM_UTIL_JAR) | dirs
	# Always clean the build directory to ensure consistency
	rm -rf $(QTAP_BUILD_DIR)
	@mkdir -p $(QTAP_BUILD_DIR)

	# Compile the Java source
	$(JAVAC) --release 8 -d $(QTAP_BUILD_DIR) -cp $(BUILD_DIR)/$(ASM_JAR):$(BUILD_DIR)/$(ASM_COMMONS_JAR):$(BUILD_DIR)/$(ASM_UTIL_JAR) $(QTAP_SRC)
	
	# Extract the ASM JARs
	(cd $(QTAP_BUILD_DIR) && $(JAR) xf ../../$(ASM_JAR))
	(cd $(QTAP_BUILD_DIR) && $(JAR) xf ../../$(ASM_COMMONS_JAR))
	(cd $(QTAP_BUILD_DIR) && $(JAR) xf ../../$(ASM_UTIL_JAR))
	
	# Create the final JAR
	$(JAR) cmf $(QTAP_MANIFEST) $@ -C $(QTAP_BUILD_DIR) .

# C targets
c: $(C_TARGET)

$(C_TARGET): $(C_OBJECTS)
	$(CC) $(CFLAGS) -o $@ $^

$(C_BUILD_DIR)/%.o: c/%.c | dirs
	$(CC) $(CFLAGS) -c -o $@ $<

# Download dependencies
$(BUILD_DIR)/$(ASM_JAR): | dirs
	curl -L $(ASM_URL) -o $@

$(BUILD_DIR)/$(ASM_COMMONS_JAR): | dirs
	curl -L $(ASM_COMMONS_URL) -o $@

$(BUILD_DIR)/$(ASM_UTIL_JAR): | dirs
	curl -L $(ASM_UTIL_URL) -o $@

install-gjf: $(GJF_JAR)

$(GJF_JAR): | dirs
	curl -L $(GJF_URL) -o $@

$(JRE_BUNDLE): | dirs
	@echo "DEBUG: Building JRE bundle at $@, dependencies: $^"
	@echo "DEBUG: JRE_BUNDLE variable is: $(JRE_BUNDLE)"
	@mkdir -p $(BUILD_DIR)
	@echo "Building JRE for $(ARCH)..."
	@set -e; \
	if [ "$(ARCH)" = "amd64" ]; then \
		DOWNLOAD_ARCH=x64; \
	elif [ "$(ARCH)" = "arm64" ]; then \
		DOWNLOAD_ARCH=aarch64; \
	else \
		echo "Unsupported architecture: $(ARCH)"; exit 1; \
	fi; \
	TAR_FILE="OpenJDK17U-jdk_$${DOWNLOAD_ARCH}_linux_hotspot_$(JRE_VERSION_UNDERSCORE).tar.gz"; \
	DOWNLOAD_URL="$(JRE_BASE_URL)/$${TAR_FILE}"; \
	echo "Downloading $${TAR_FILE}..."; \
	curl -L "$${DOWNLOAD_URL}" -o "$(BUILD_DIR)/$${TAR_FILE}"; \
	echo "Extracting JDK..."; \
	TEMP_DIR=$$(mktemp -d); \
	trap 'rm -rf "$$TEMP_DIR" "$(BUILD_DIR)/$${TAR_FILE}"' EXIT; \
	mkdir -p "$$TEMP_DIR/jdk"; \
	tar -xzf "$(BUILD_DIR)/$${TAR_FILE}" -C "$$TEMP_DIR/jdk" --strip-components=1; \
	echo "Creating minimal JRE with jlink..."; \
	"$$TEMP_DIR/jdk/bin/jlink" \
		--module-path "$$TEMP_DIR/jdk/jmods" \
		--add-modules java.base,jdk.attach \
		--output "$$TEMP_DIR/custom-jre" \
		--strip-debug \
		--ignore-signing-information \
		--no-header-files \
		--no-man-pages; \
	echo "Creating compressed bundle..."; \
	tar -czf $@ -C "$$TEMP_DIR" custom-jre; \
	echo "JRE bundle created: $@"

.PHONY: fmt
fmt: install-gjf ## Format code
	@echo $(INFO) Formatting C code... $(RESET)
	@find ./c -type f -name "*.c" | xargs clang-format -i --Werror
	@echo $(SUCCESS) Format C complete! $(RESET)
	@echo $(INFO) Formatting Java code... $(RESET)
	@java $(GJF_JVM_FLAGS) -jar $(GJF_JAR) --replace $(GJF_JAVA_FILES)
	@echo $(SUCCESS) Format Java complete! $(RESET)

check-format: install-gjf ## Check Java code formatting
	@echo $(INFO) Checking Java code formatting... $(RESET)
	@java $(GJF_JVM_FLAGS) -jar $(GJF_JAR) --dry-run --set-exit-if-changed $(GJF_JAVA_FILES) || (echo "Java code is not properly formatted. Run 'make fmt' to fix." && exit 1)
	@echo $(SUCCESS) Java formatting check passed! $(RESET)

clean:
	rm -rf $(BUILD_DIR)
	rm -rf *.jar 
