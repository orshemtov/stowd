BIN = $(HOME)/.local/bin/stowd
DOTFILES_DIR ?= $(HOME)/Projects/dotfiles
TARGET_DIR ?= $(HOME)

build:
	go build -o $(BIN)

install-user: build
	@DOTFILES_DIR=$(DOTFILES_DIR) TARGET_DIR=$(TARGET_DIR) ./scripts/install-user.sh

uninstall-user:
	./scripts/uninstall-user.sh

logs:
	tail -f $(HOME)/Library/Logs/stowd.out.log $(HOME)/Library/Logs/stowd.err.log
