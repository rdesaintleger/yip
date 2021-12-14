// Copyright © 2020 Ettore Di Giacinto <mudler@gentoo.org>
//
// This program is free software; you can redistribute it and/or modify
// it under the terms of the GNU General Public License as published by
// the Free Software Foundation; either version 2 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU General Public License for more details.
//
// You should have received a copy of the GNU General Public License along
// with this program; if not, see <http://www.gnu.org/licenses/>.

package executor

import (
	"os"
	"path/filepath"

	"github.com/hashicorp/go-multierror"
	"github.com/mudler/yip/pkg/logger"
	"github.com/mudler/yip/pkg/plugins"
	"github.com/mudler/yip/pkg/schema"
	"github.com/mudler/yip/pkg/utils"
	"github.com/twpayne/go-vfs"
)

// DefaultExecutor is the default yip Executor.
// It simply creates file and executes command for a linux executor
type DefaultExecutor struct {
	plugins      []Plugin
	conditionals []Plugin
	modifier     schema.Modifier
	logger       logger.Interface
}

func (e *DefaultExecutor) Plugins(p []Plugin) {
	e.plugins = p
}

func (e *DefaultExecutor) Conditionals(p []Plugin) {
	e.conditionals = p
}

func (e *DefaultExecutor) Modifier(m schema.Modifier) {
	e.modifier = m
}

func (e *DefaultExecutor) walkDir(stage, dir string, fs vfs.FS, console plugins.Console) error {
	var errs error

	err := vfs.Walk(fs, dir,
		func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if path == dir {
				return nil
			}
			// Process only files
			if info.IsDir() {
				return nil
			}
			ext := filepath.Ext(path)
			if ext != ".yaml" && ext != ".yml" {
				return nil
			}

			if err = e.run(stage, path, fs, console, schema.FromFile, e.modifier); err != nil {
				errs = multierror.Append(errs, err)
				return nil
			}

			return nil
		})
	if err != nil {
		errs = multierror.Append(errs, err)
	}
	return errs
}

func (e *DefaultExecutor) run(stage, uri string, fs vfs.FS, console plugins.Console, l schema.Loader, m schema.Modifier) error {
	config, err := schema.Load(uri, fs, l, m)
	if err != nil {
		return err
	}

	e.logger.Infof("Executing %s", uri)
	if err = e.Apply(stage, *config, fs, console); err != nil {
		return err
	}

	return nil
}

func (e *DefaultExecutor) runStage(stage, uri string, fs vfs.FS, console plugins.Console) (err error) {
	f, err := fs.Stat(uri)

	switch {
	case err == nil && f.IsDir():
		err = e.walkDir(stage, uri, fs, console)
	case err == nil:
		err = e.run(stage, uri, fs, console, schema.FromFile, e.modifier)
	case utils.IsUrl(uri):
		err = e.run(stage, uri, fs, console, schema.FromUrl, e.modifier)
	default:

		err = e.run(stage, uri, fs, console, nil, e.modifier)
	}

	return
}

// Run takes a list of URI to run yipfiles from. URI can be also a dir or a local path, as well as a remote
func (e *DefaultExecutor) Run(stage string, fs vfs.FS, console plugins.Console, args ...string) error {
	var errs error
	e.logger.Infof("Executing stage: %s\n", stage)
	for _, source := range args {
		if err := e.runStage(stage, source, fs, console); err != nil {
			errs = multierror.Append(errs, err)
		}
	}
	e.logger.Infof("Done executing stage '%s'. Errors: %t\n", stage, errs != nil)
	return errs
}

// Apply applies a yip Config file by creating files and running commands defined.
func (e *DefaultExecutor) Apply(stageName string, s schema.YipConfig, fs vfs.FS, console plugins.Console) error {

	currentStages := s.Stages[stageName]
	var errs error

	e.logger.Infof("Executing file name: %s stages: %d stage: %s\n",
		s.Name, len(currentStages), stageName)

STAGES:
	for _, stage := range currentStages {
		for _, p := range e.conditionals {
			if err := p(e.logger, stage, fs, console); err != nil {
				e.logger.Warnf("Error '%s' in stage name: %s stage: %s\n",
					err.Error(), s.Name, stageName)
				continue STAGES
			}
		}

		e.logger.Infof(
			"Processing stage step '%s'. commands: %d entities: %d nameserver: %d files: %d delete_entities: %d\n",
			stage.Name,
			len(stage.Commands),
			len(stage.EnsureEntities),
			len(stage.Dns.Nameservers),
			len(stage.Files),
			len(stage.DeleteEntities))

		for _, p := range e.plugins {
			if err := p(e.logger, stage, fs, console); err != nil {
				e.logger.Error(err.Error())
				errs = multierror.Append(errs, err)
			}
		}
	}

	e.logger.Infof(
		"stage '%s' stages: %d success: %t\n",
		stageName,
		len(currentStages),
		errs == nil,
	)

	return errs
}
