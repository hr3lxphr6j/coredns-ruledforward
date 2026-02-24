# CoreDNS ruledforward plugin - Build, test and package
# 支持：编译测试、OpenWrt/Alpine/deb/rpm/pacman 包、Docker 镜像，多架构

BINARY     ?= coredns
VERSION    ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "0.0.0-dev")
COREDNS_VER?= v1.14.1
BUILD_DIR  ?= .build
DIST_DIR   ?= dist
PACK_DIR   ?= $(BUILD_DIR)/pack
PLUGIN_REPO?= github.com/hr3lxphr6j/coredns-ruledforward
# 当前插件源码路径，用于 go.mod replace（构建时传入或自动检测）
REPO_PATH  ?= $(shell pwd)
PWD        ?= $(CURDIR)
GO         ?= go

# 主流架构：amd64, arm64, arm(v6/v7), 386
GOARCHES   := amd64 arm64 arm 386

# Docker 多架构
DOCKER_PLATFORMS ?= linux/amd64,linux/arm64,linux/arm/v7,linux/arm/v6,linux/386

.PHONY: all build test get-coredns clean pack-deb pack-rpm pack-apk pack-pacman pack-openwrt docker integration debug help generate
.PHONY: build-arch-ci pack-apk-alpine pack-pacman-arch docker-release

all: build

# Generate Go code from proto (dlc.dat GeoSiteList). Requires protoc and protoc-gen-go.
generate:
	@mkdir -p internal/dlcpb
	@command -v protoc >/dev/null 2>&1 || (echo "protoc not found, install protocolbuffers/protobuf"; exit 1)
	@command -v protoc-gen-go >/dev/null 2>&1 || (echo "protoc-gen-go not found, run: go install google.golang.org/protobuf/cmd/protoc-gen-go@latest"; exit 1)
	protoc --go_out=. --go_opt=module=$(PLUGIN_REPO) proto/geosite.proto

help:
	@echo "Targets:"
	@echo "  generate       - 从 proto 生成 Go 代码 (需 protoc + protoc-gen-go)"
	@echo "  build          - 编译当前架构二进制"
	@echo "  build-all      - 编译所有架构二进制"
	@echo "  test           - 运行单元测试"
	@echo "  get-coredns    - 下载并注入插件的 CoreDNS 源码到 $(BUILD_DIR)/coredns"
	@echo "  debug          - 启动 dlv debugger (headless, listen :2345) 用于本地开发调试"
	@echo "  pack-deb       - 生成 .deb 包"
	@echo "  pack-rpm       - 生成 .rpm 包"
	@echo "  pack-apk       - 生成 Alpine .apk 包"
	@echo "  pack-pacman    - 生成 Arch .pkg.tar.zst 包"
	@echo "  pack-openwrt   - 生成 OpenWrt ipk 包"
	@echo "  docker         - 构建多架构 Docker 镜像"
	@echo "  docker-release - 使用 Dockerfile.CI 与 dist/bin 构建并推送 (CI)"
	@echo "  integration    - Docker 内运行集成测试 (dig + metrics)"
	@echo "  clean          - 清理构建产物"
	@echo "  build-arch-ci  - 单架构输出到 dist/bin (CI; 需 GOOS, GOARCH, VERSION; arm 需 GOARM)"
	@echo "  pack-apk-alpine - Alpine 容器内打 .apk (CI; 需 STAGING, VERSION, ARCH)"
	@echo "  pack-pacman-arch - Arch 容器内打 .pkg.tar.zst (CI; 需 STAGING, VERSION, ARCH)"

# 下载 CoreDNS 并在 plugin.cfg 中注入 ruledforward（放在 forward 前）
# 需网络；macOS/Linux 通用（用 perl 做插入，避免 GNU/macOS sed 差异）
get-coredns:
	@mkdir -p $(BUILD_DIR)
	@if [ ! -d "$(BUILD_DIR)/coredns" ]; then \
		echo "Cloning CoreDNS $(COREDNS_VER)..."; \
		git clone --depth 1 --branch $(COREDNS_VER) https://github.com/coredns/coredns.git $(BUILD_DIR)/coredns; \
	fi
	@if [ ! -f "$(BUILD_DIR)/coredns/plugin.cfg" ]; then \
		echo "Removing incomplete $(BUILD_DIR)/coredns and re-cloning..."; \
		rm -rf "$(BUILD_DIR)/coredns" && \
		git clone --depth 1 --branch $(COREDNS_VER) https://github.com/coredns/coredns.git $(BUILD_DIR)/coredns; \
	fi
	@if [ ! -f "$(BUILD_DIR)/coredns/plugin.cfg" ]; then \
		echo "Error: clone failed or $(BUILD_DIR)/coredns/plugin.cfg missing. Check network and run: make clean && make all"; exit 1; \
	fi
	@cd $(BUILD_DIR)/coredns && \
		(grep -q 'ruledforward:' plugin.cfg) || \
		perl -i.bak -ne 'print "ruledforward:$(PLUGIN_REPO)\n" if /^forward:/; print' plugin.cfg
	@cd $(BUILD_DIR)/coredns && \
		(grep -q 'replace.*coredns-ruledforward' go.mod) || \
		(echo '' >> go.mod && echo 'replace $(PLUGIN_REPO) => $(REPO_PATH)' >> go.mod)
	@$(MAKE) -f $(PWD)/Makefile coredns-gen

# 在 coredns 目录内执行 go generate（仅根目录 coredns.go）。必须用本机 GOOS/GOARCH 运行，
# 否则 build-all 交叉编译时 go run directives_generate 会生成目标平台二进制导致 exec format error
coredns-gen:
	@cd $(BUILD_DIR)/coredns && GOOS= GOARCH= GOARM= $(GO) generate coredns.go
	@cd $(BUILD_DIR)/coredns && GOOS= GOARCH= GOARM= $(GO) get

# 启动 dlv debugger (headless) 用于本地开发调试
# 用法: make debug [COREDNS_ARGS="-conf /path/to/Corefile"]
# 然后使用 IDE (如 VS Code/Cursor) 连接到 localhost:2345 进行调试
# 示例: make debug COREDNS_ARGS="-conf test/Corefile"
debug: get-coredns
	@if ! command -v dlv >/dev/null 2>&1; then \
		echo "Error: dlv (Delve) not found. Install with: go install github.com/go-delve/delve/cmd/dlv@latest"; \
		exit 1; \
	fi
	@echo "Starting dlv debugger (headless) on :2345..."
	@echo "Connect your IDE debugger to localhost:2345"
	@echo "CoreDNS args: $(COREDNS_ARGS)"
	@echo "Press Ctrl+C to stop"
	@cd $(BUILD_DIR)/coredns && dlv debug --headless --listen=:2345 --api-version=2 --accept-multiclient . -- $(COREDNS_ARGS)

# 编译当前架构
build: get-coredns
	@rm -f $(PWD)/$(BINARY)
	@cd $(BUILD_DIR)/coredns && CGO_ENABLED=0 $(GO) build -ldflags "-s -w -X github.com/coredns/coredns/coremain.GitCommit=$(VERSION)" -o $(PWD)/$(BINARY) . || (echo "*** go build failed ***"; exit 1)
	@if [ ! -f $(PWD)/$(BINARY) ]; then echo "Error: no binary produced. Fix go build errors above."; exit 1; fi
	@if ! file $(PWD)/$(BINARY) | grep -qE 'executable|Mach-O|ELF'; then \
		rm -f $(PWD)/$(BINARY); \
		echo "Error: build produced non-executable. Remove any .deb/ar file named $(BINARY) and run make all again."; exit 1; \
	fi
	@echo "Built: $(BINARY)"

# 编译指定架构（用法: make build-arch GOOS=linux GOARCH=arm GOARM=7）
build-arch: get-coredns
	@cd $(BUILD_DIR)/coredns && \
		CGO_ENABLED=0 GOOS=$(GOOS) GOARCH=$(GOARCH) GOARM=$(GOARM) $(GO) build -ldflags "-s -w -X github.com/coredns/coredns/coremain.GitCommit=$(VERSION)" -o $(PWD)/$(BINARY)-$(GOOS)-$(GOARCH)$(GOARM) .
	@echo "Built: $(BINARY)-$(GOOS)-$(GOARCH)$(GOARM)"

# CI/Release：编译指定架构并输出到 DIST_DIR/bin（用法: make build-arch-ci GOOS=linux GOARCH=amd64 VERSION=v1.0.0）
# 使用 $(PWD)/ 保证输出到仓库根下的 dist/bin，而非 .build/coredns 下的相对路径
build-arch-ci: get-coredns
	@mkdir -p $(PWD)/$(DIST_DIR)/bin
	@cd $(BUILD_DIR)/coredns && \
		CGO_ENABLED=0 GOOS=$(GOOS) GOARCH=$(GOARCH) GOARM=$(GOARM) $(GO) build -ldflags "-s -w -X github.com/coredns/coredns/coremain.GitCommit=$(VERSION)" -o $(PWD)/$(DIST_DIR)/bin/$(BINARY)-$(GOOS)-$(GOARCH)$(GOARM) .
	@echo "Built: $(DIST_DIR)/bin/$(BINARY)-$(GOOS)-$(GOARCH)$(GOARM)"

# 编译所有常见 linux 架构
build-all: get-coredns
	@mkdir -p $(DIST_DIR)/bin
	@for arch in $(GOARCHES); do \
		ARM="" ; \
		case $$arch in arm) ARM="6 7";; *) ARM="";; esac; \
		if [ -z "$$ARM" ]; then \
			$(MAKE) build-arch GOOS=linux GOARCH=$$arch && mv $(BINARY)-linux-$$arch $(DIST_DIR)/bin/$(BINARY)-linux-$$arch; \
		else \
			for goarm in $$ARM; do \
				$(MAKE) build-arch GOOS=linux GOARCH=arm GOARM=$$goarm && mv $(BINARY)-linux-arm$$goarm $(DIST_DIR)/bin/; \
			done; \
		fi; \
	done
	@echo "All binaries in $(DIST_DIR)/bin/"

test:
	$(GO) test -v ./...

# Docker 内集成测试：需 Docker 与 docker compose
integration:
	@chmod +x integration/scripts/run.sh integration/scripts/run_tests.sh 2>/dev/null || true
	@./integration/scripts/run.sh

clean:
	rm -rf $(BUILD_DIR) $(DIST_DIR) $(BINARY) $(BINARY)-*

# ---------- 打包用：安装路径与通用文件 ----------
PREFIX     ?= /usr
BINDIR     ?= $(PREFIX)/bin
SYSTEMD_DIR?= $(PREFIX)/lib/systemd/system
SYSUSERS_DIR?= $(PREFIX)/lib/sysusers.d
MAN8_DIR   ?= $(PREFIX)/share/man/man8

# ---------- .deb (dpkg) ----------
# pack-deb-only：仅打包，要求已存在 dist/bin/ 下二进制（CI 用）
pack-deb-only:
	@command -v dpkg-deb >/dev/null 2>&1 || (echo "Need dpkg"; exit 1)
	@for bin in $(DIST_DIR)/bin/$(BINARY)-*; do \
		[ -f "$$bin" ] || continue; \
		suffix=$$(basename $$bin | sed "s/^$(BINARY)-//"); \
		case $$suffix in \
			linux-amd64) pkg_arch=amd64;; linux-arm64) pkg_arch=arm64;; linux-arm6) pkg_arch=armhf;; linux-arm7) pkg_arch=armhf;; linux-386) pkg_arch=i386;; *) pkg_arch=$$suffix;; \
		esac; \
		deb_ver=$$(echo "$(VERSION)" | sed 's/^v//'); \
		staging=$(PACK_DIR)/deb-$$suffix; \
		rm -rf $$staging; \
		mkdir -p $$staging/$(BINDIR) $$staging/$(SYSTEMD_DIR) $$staging/$(SYSUSERS_DIR) $$staging/etc/init.d $$staging/$(MAN8_DIR) $$staging/$(PREFIX)/share/man/zh_CN/man8; \
		cp $$bin $$staging/$(BINDIR)/$(BINARY); \
		chmod 755 $$staging/$(BINDIR)/$(BINARY); \
		cp $(PWD)/dist/systemd/coredns.service $$staging/$(SYSTEMD_DIR)/; \
		cp $(PWD)/dist/systemd/coredns-sysusers.conf $$staging/$(SYSUSERS_DIR)/coredns.conf; \
		cp $(PWD)/dist/sysvinit/coredns $$staging/etc/init.d/; \
		chmod 755 $$staging/etc/init.d/coredns; \
		cp $(PWD)/man/coredns-ruledforward.8 $$staging/$(MAN8_DIR)/coredns.8; \
		cp $(PWD)/man/coredns-ruledforward.8.zh $$staging/$(PREFIX)/share/man/zh_CN/man8/coredns.8; \
		mkdir -p $$staging/DEBIAN; \
		cp $(PWD)/dist/deb/postinst $$staging/DEBIAN/; chmod 755 $$staging/DEBIAN/postinst; \
		echo "Package: $(BINARY)" > $$staging/DEBIAN/control; \
		echo "Version: $$deb_ver" >> $$staging/DEBIAN/control; \
		echo "Architecture: $$pkg_arch" >> $$staging/DEBIAN/control; \
		echo "Maintainer: coredns" >> $$staging/DEBIAN/control; \
		echo "Description: CoreDNS with ruledforward plugin" >> $$staging/DEBIAN/control; \
		dpkg-deb --build --root-owner-group $$staging $(DIST_DIR)/$(BINARY)_$$deb_ver_$$pkg_arch.deb; \
	done
	@echo "Debs in $(DIST_DIR)/"

pack-deb: build-all pack-deb-only

# ---------- .rpm (rpmbuild) ----------
pack-rpm-only:
	@command -v rpmbuild >/dev/null 2>&1 || (echo "Need rpmbuild"; exit 1)
	@mkdir -p $(BUILD_DIR)/rpmbuild/BUILD $(BUILD_DIR)/rpmbuild/RPMS $(BUILD_DIR)/rpmbuild/SOURCES $(BUILD_DIR)/rpmbuild/SPECS
	@for bin in $(DIST_DIR)/bin/$(BINARY)-*; do \
		[ -f "$$bin" ] || continue; \
		suffix=$$(basename $$bin | sed "s/^$(BINARY)-//"); \
		case $$suffix in linux-amd64) pkg_arch=x86_64;; linux-arm64) pkg_arch=aarch64;; linux-arm6) pkg_arch=armv7hl;; linux-arm7) pkg_arch=armv7hl;; linux-386) pkg_arch=i686;; *) pkg_arch=$$suffix;; esac; \
		rpm_ver=$$(echo "$(VERSION)" | sed 's/^v//'); \
		rpm_version=$$(echo "$$rpm_ver" | cut -d- -f1); \
		rpm_release=$$(echo "$$rpm_ver" | cut -d- -s -f2-); \
		if [ -z "$$rpm_release" ]; then rpm_release=1; else case "$$rpm_release" in *[!0-9]*) rpm_release=0.$$rpm_release;; esac; fi; \
		staging=$(PACK_DIR)/rpm-$$suffix; \
		rm -rf $$staging $(BUILD_DIR)/rpmbuild/BUILD/*; \
		mkdir -p $$staging/$(BINDIR) $$staging/$(SYSTEMD_DIR) $$staging/$(SYSUSERS_DIR) $$staging/etc/init.d $$staging/$(MAN8_DIR) $$staging/$(PREFIX)/share/man/zh_CN/man8; \
		cp $$bin $$staging/$(BINDIR)/$(BINARY); \
		chmod 755 $$staging/$(BINDIR)/$(BINARY); \
		cp $(PWD)/dist/systemd/coredns.service $$staging/$(SYSTEMD_DIR)/; \
		cp $(PWD)/dist/systemd/coredns-sysusers.conf $$staging/$(SYSUSERS_DIR)/coredns.conf; \
		cp $(PWD)/dist/sysvinit/coredns $$staging/etc/init.d/; \
		chmod 755 $$staging/etc/init.d/coredns; \
		cp $(PWD)/man/coredns-ruledforward.8 $$staging/$(MAN8_DIR)/coredns.8; \
		cp $(PWD)/man/coredns-ruledforward.8.zh $$staging/$(PREFIX)/share/man/zh_CN/man8/coredns.8; \
		cp -a $$staging/* $(BUILD_DIR)/rpmbuild/BUILD/; \
		mkdir -p $(BUILD_DIR)/rpmbuild/BUILD/usr/share/man/man8 $(BUILD_DIR)/rpmbuild/BUILD/usr/share/man/zh_CN/man8; \
		cp "$$(pwd)/man/coredns-ruledforward.8" $(BUILD_DIR)/rpmbuild/BUILD/usr/share/man/man8/coredns.8; \
		cp "$$(pwd)/man/coredns-ruledforward.8.zh" $(BUILD_DIR)/rpmbuild/BUILD/usr/share/man/zh_CN/man8/coredns.8; \
		sed -e 's/{{VERSION}}/'"$$rpm_version"'/g' -e 's/{{RELEASE}}/'"$$rpm_release"'/g' -e 's/{{ARCH}}/'"$$pkg_arch"'/g' $(PWD)/dist/rpm/$(BINARY).spec.in > $(BUILD_DIR)/rpmbuild/SPECS/$(BINARY).spec; \
		rpmbuild -bb --define "_topdir $(PWD)/$(BUILD_DIR)/rpmbuild" $(BUILD_DIR)/rpmbuild/SPECS/$(BINARY).spec; \
		cp $(BUILD_DIR)/rpmbuild/RPMS/*/*.rpm $(DIST_DIR)/; \
	done
	@echo "Rpms in $(DIST_DIR)/"

pack-rpm: build-all pack-rpm-only

# ---------- Alpine .apk (需在 Alpine 容器内用 abuild 打正式 apk) ----------
# pack-apk-only：仅生成 staging，要求已存在 dist/bin/ 下二进制（CI 用）
pack-apk-only:
	@for bin in $(DIST_DIR)/bin/$(BINARY)-*; do \
		[ -f "$$bin" ] || continue; \
		suffix=$$(basename $$bin | sed "s/^$(BINARY)-//"); \
		staging=$(PACK_DIR)/apk-$$suffix; \
		rm -rf $$staging; \
		mkdir -p $$staging/$(BINDIR) $$staging/$(SYSTEMD_DIR) $$staging/$(SYSUSERS_DIR) $$staging/etc/init.d $$staging/$(MAN8_DIR) $$staging/$(PREFIX)/share/man/zh_CN/man8; \
		cp $$bin $$staging/$(BINDIR)/$(BINARY); \
		chmod 755 $$staging/$(BINDIR)/$(BINARY); \
		cp $(PWD)/dist/systemd/coredns.service $$staging/$(SYSTEMD_DIR)/; \
		cp $(PWD)/dist/systemd/coredns-sysusers.conf $$staging/$(SYSUSERS_DIR)/coredns.conf; \
		cp $(PWD)/dist/sysvinit/coredns $$staging/etc/init.d/; \
		chmod 755 $$staging/etc/init.d/coredns; \
		cp $(PWD)/man/coredns-ruledforward.8 $$staging/$(MAN8_DIR)/coredns.8; \
		cp $(PWD)/man/coredns-ruledforward.8.zh $$staging/$(PREFIX)/share/man/zh_CN/man8/coredns.8; \
	done
	@echo "Staging in $(PACK_DIR)/apk-* ; 在 Alpine 内运行 dist/apk/build-apk.sh 生成 .apk"

pack-apk: build-all pack-apk-only

# CI/Release：在 Alpine 容器内为单个架构打 .apk（需先 make pack-apk-only；用法: make pack-apk-alpine STAGING=.build/pack/apk-linux-amd64 VERSION=v1.0.0 ARCH=x86_64）
pack-apk-alpine:
	@[ -n "$(STAGING)" ] && [ -n "$(VERSION)" ] && [ -n "$(ARCH)" ] || (echo "Need STAGING, VERSION, ARCH"; exit 1)
	@[ -d "$(STAGING)" ] || (echo "Staging $(STAGING) not found"; exit 1)
	@[ -f $(PWD)/dist/apk/APKBUILD ] || (echo "dist/apk/APKBUILD not found"; exit 1)
	@docker run --rm -v "$(PWD)":/work -w /work -e STAGING="$(STAGING)" -e VERSION="$(VERSION)" -e ARCH="$(ARCH)" alpine:latest sh -c ' \
		APK_VER=$$(echo "$$VERSION" | sed "s/^v//;s/-/_/g") && \
		apk add --no-cache abuild alpine-sdk && \
		addgroup -S abuild && \
		adduser -D builder && \
		adduser builder abuild && \
		printf "\n" | su builder -c "abuild-keygen -a" && \
		cp /home/builder/.abuild/*.rsa.pub /etc/apk/keys/ && \
		mkdir -p /tmp/apkbuild /tmp/apkbuild/pkg && \
		cp -r /work/'"$(STAGING)"'/* /tmp/apkbuild/pkg/ && \
		sed "s/{{VERSION}}/$$APK_VER/g; s/{{ARCH}}/$$ARCH/g" /work/dist/apk/APKBUILD > /tmp/apkbuild/APKBUILD && \
		chown -R builder:builder /tmp/apkbuild && \
		cd /tmp/apkbuild && su builder -c "PACKAGER=coredns abuild -P /work/'"$(DIST_DIR)"'" 2>&1 || { \
			echo "abuild failed, creating tar.gz fallback"; tar -czf /work/'"$(DIST_DIR)"'/$(BINARY)-'"$(VERSION)"'-'"$(ARCH)"'-apk-staging.tar.gz -C /work/'"$(STAGING)"' .; \
		}'
	@echo "APK or fallback in $(DIST_DIR)/"
	@echo "Note: *-apk-staging.tar.gz is NOT an apk; use only if abuild failed. Extract and copy usr/ etc/ to root, then create coredns user."

# ---------- Pacman (Arch)：生成 staging，在 Arch 容器内用 PKGBUILD 打 .pkg.tar.zst ----------
# pack-pacman-only：仅生成 staging，要求已存在 dist/bin/ 下二进制（CI 用）
pack-pacman-only:
	@for bin in $(DIST_DIR)/bin/$(BINARY)-*; do \
		[ -f "$$bin" ] || continue; \
		suffix=$$(basename $$bin | sed "s/^$(BINARY)-//"); \
		staging=$(PACK_DIR)/pacman-$$suffix; \
		rm -rf $$staging; \
		mkdir -p $$staging/$(BINDIR) $$staging/$(SYSTEMD_DIR) $$staging/$(SYSUSERS_DIR) $$staging/etc/init.d $$staging/$(MAN8_DIR) $$staging/$(PREFIX)/share/man/zh_CN/man8; \
		cp $$bin $$staging/$(BINDIR)/$(BINARY); \
		chmod 755 $$staging/$(BINDIR)/$(BINARY); \
		cp $(PWD)/dist/systemd/coredns.service $$staging/$(SYSTEMD_DIR)/; \
		cp $(PWD)/dist/systemd/coredns-sysusers.conf $$staging/$(SYSUSERS_DIR)/coredns.conf; \
		cp $(PWD)/dist/sysvinit/coredns $$staging/etc/init.d/; \
		chmod 755 $$staging/etc/init.d/coredns; \
		cp $(PWD)/man/coredns-ruledforward.8 $$staging/$(MAN8_DIR)/coredns.8; \
		cp $(PWD)/man/coredns-ruledforward.8.zh $$staging/$(PREFIX)/share/man/zh_CN/man8/coredns.8; \
	done
	@echo "Staging in $(PACK_DIR)/pacman-* ; 在 Arch 内用 dist/pacman/PKGBUILD 打 .pkg.tar.zst"

pack-pacman: build-all pack-pacman-only

# CI/Release：在 Arch 容器内为单个架构打 .pkg.tar.zst（需先 make pack-pacman-only；用法: make pack-pacman-arch STAGING=.build/pack/pacman-linux-amd64 VERSION=v1.0.0 ARCH=x86_64）
pack-pacman-arch:
	@[ -n "$(STAGING)" ] && [ -n "$(VERSION)" ] && [ -n "$(ARCH)" ] || (echo "Need STAGING, VERSION, ARCH"; exit 1)
	@[ -d "$(STAGING)" ] || (echo "Staging $(STAGING) not found"; exit 1)
	@docker run --rm -v "$(PWD)":/work -w /work -e STAGING="$(STAGING)" -e VERSION="$(VERSION)" -e ARCH="$(ARCH)" archlinux:latest sh -c ' \
		PKGVER=$$(echo "$$VERSION" | sed "s/^v//;s/-/_/g") && \
		pacman -Sy --noconfirm base-devel && \
		useradd -m -s /bin/bash builder 2>/dev/null || true && \
		mkdir -p /tmp/pkgbuild/prebuilt && cp -r /work/'"$(STAGING)"'/* /tmp/pkgbuild/prebuilt/ && \
		cp /work/dist/pacman/coredns.install /tmp/pkgbuild/ && \
		sed "s/{{VERSION}}/$$PKGVER/g; s/{{ARCH}}/$$ARCH/g" /work/dist/pacman/PKGBUILD > /tmp/pkgbuild/PKGBUILD && \
		chown -R builder:builder /tmp/pkgbuild && cd /tmp/pkgbuild && \
		su builder -c "makepkg --noconfirm --cleanbuild --clean" 2>&1 || { \
			echo "makepkg failed, creating tar.gz fallback"; cd /tmp/pkgbuild && tar -czf /work/'"$(DIST_DIR)"'/$(BINARY)-'"$(VERSION)"'-'"$(ARCH)"'-pacman-staging.tar.gz .; \
		} && cp *.pkg.tar.zst /work/'"$(DIST_DIR)"'/ 2>/dev/null || true'
	@echo "Pacman or fallback in $(DIST_DIR)/"

# ---------- OpenWrt ipk：生成 staging，在 OpenWrt SDK 中打包 ----------
# pack-openwrt-staging：仅生成 staging，要求已存在 dist/bin/ 下二进制（CI 用）
pack-openwrt-staging:
	@for bin in $(DIST_DIR)/bin/$(BINARY)-*; do \
		[ -f "$$bin" ] || continue; \
		suffix=$$(basename $$bin | sed "s/^$(BINARY)-//"); \
		staging=$(PACK_DIR)/openwrt-$$suffix; \
		rm -rf $$staging; mkdir -p $$staging/usr/bin $$staging/etc/init.d $$staging/usr/share/man/man8; \
		cp $$bin $$staging/usr/bin/$(BINARY); \
		chmod 755 $$staging/usr/bin/$(BINARY); \
		cp $(PWD)/dist/sysvinit/coredns $$staging/etc/init.d/; \
		chmod 755 $$staging/etc/init.d/coredns; \
		cp $(PWD)/man/coredns-ruledforward.8 $$staging/usr/share/man/man8/coredns.8; \
	done
	@echo "Staging in $(PACK_DIR)/openwrt-* ; 在 OpenWrt SDK 中打 ipk，见 dist/openwrt/"

pack-openwrt: build-all pack-openwrt-staging

# ---------- OpenWrt ipk：直接生成 .ipk 包 ----------
pack-openwrt-only:
	@if [ -z "$(VERSION)" ]; then \
		echo "Error: VERSION is required. Usage: make pack-openwrt-only VERSION=1.0.0"; \
		exit 1; \
	fi
	@chmod +x $(PWD)/dist/openwrt/build-ipk.sh
	@for bin in $(DIST_DIR)/bin/$(BINARY)-*; do \
		[ -f "$$bin" ] || continue; \
		suffix=$$(basename $$bin | sed "s/^$(BINARY)-//"); \
		staging=$(PACK_DIR)/openwrt-$$suffix; \
		if [ ! -d "$$staging" ]; then \
			echo "Staging $$staging not found. Run 'make pack-openwrt' first."; \
			continue; \
		fi; \
		arch=$$(echo $$suffix | sed 's/linux-//'); \
		$(PWD)/dist/openwrt/build-ipk.sh "$$staging" "$(VERSION)" "$$arch" "$(DIST_DIR)"; \
	done
	@echo "IPK packages in $(DIST_DIR)/"

# ---------- Docker 多架构（CI 中通常设 DOCKER_IMAGE 与 --push） ----------
docker: build-all
	@docker buildx create --use --name coredns-builder 2>/dev/null || true
	@docker buildx build --platform $(DOCKER_PLATFORMS) \
		-t $(DOCKER_IMAGE):$(VERSION) -t $(DOCKER_IMAGE):latest \
		-f Dockerfile $(DOCKER_PUSH_FLAG) .
	@echo "Image: $(DOCKER_IMAGE):$(VERSION)"

# CI/Release：使用 Dockerfile.CI 与已存在的 dist/bin 构建并推送（不执行 build-all；用法: make docker-release DOCKER_IMAGE=ghcr.io/owner/repo VERSION=v1.0.0 DOCKER_PUSH_FLAG=--push）
docker-release:
	@[ -n "$(DOCKER_IMAGE)" ] && [ -n "$(VERSION)" ] || (echo "Need DOCKER_IMAGE, VERSION"; exit 1)
	@docker buildx build --platform $(DOCKER_PLATFORMS) \
		-t $(DOCKER_IMAGE):$(VERSION) -t $(DOCKER_IMAGE):latest \
		-f Dockerfile.CI $(DOCKER_PUSH_FLAG) .
	@echo "Image: $(DOCKER_IMAGE):$(VERSION)"

# 本地默认不 push；CI 中设置 DOCKER_IMAGE 和 DOCKER_PUSH_FLAG=--push
DOCKER_IMAGE ?= coredns
DOCKER_PUSH_FLAG ?=
