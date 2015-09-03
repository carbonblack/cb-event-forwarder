__author__ = 'jgarman'

from cbeventforwarder import version

from distutils.core import setup
from distutils.core import Command
from distutils.command.bdist_rpm import bdist_rpm

from distutils import log
from distutils.file_util import write_file
from distutils.util import change_root, convert_path

import os
from subprocess import call


class bdist_binaryrpm(bdist_rpm):
    description = "create a Cb Open Source Binary RPM distribution"

    def initialize_options(self):
        pass

    def finalize_options(self):
        pass

    def run(self):
        sdist = self.reinitialize_command('sdist')
        self.run_command('sdist')
        source = sdist.get_archive_files()[0]
        self.copy_file(source, os.path.join(os.getenv("HOME"), "rpmbuild", "SOURCES"))

        # Lots TODO here: generate spec file on demand from the rest of this setup.py file, for starters...
        # self._make_spec_file()
        call(['rpmbuild', '-bb', '%s.spec' % self.distribution.get_name()])


"""This install_cb plugin will install all data files associated with the
tool as well as the pyinstaller-compiled single binary scripts so that
they can be packaged together in a binary RPM."""
class install_cb(Command):
    description = "install binary distribution files"

    user_options = [
        ('install-dir=', 'd',
         "base directory for installing data files "
         "(default: installation base dir)"),
        ('root=', None,
         "install everything relative to this alternate root directory"),
        ('force', 'f', "force installation (overwrite existing files)"),
        ('record=', None,
         "filename in which to record list of installed files"),
        ]

    boolean_options = ['force']

    def initialize_options(self):
        self.install_dir = None
        self.outfiles = []
        self.root = None
        self.force = 0
        self.data_files = self.distribution.data_files
        self.warn_dir = 1
        self.record = None

    def finalize_options(self):
        self.set_undefined_options('install',
                                   ('install_data', 'install_dir'),
                                   ('root', 'root'),
                                   ('force', 'force'),
                                  )

    def run(self):
        for f in self.data_files:
            if isinstance(f, str):
                # don't copy files without path information
                pass
            else:
                # it's a tuple with path to install to and a list of files
                dir = convert_path(f[0])
                if not os.path.isabs(dir):
                    dir = os.path.join(self.install_dir, dir)
                elif self.root:
                    dir = change_root(self.root, dir)
                self.mkpath(dir)

                if f[1] == []:
                    # If there are no files listed, the user must be
                    # trying to create an empty directory, so add the
                    # directory to the list of output files.
                    self.outfiles.append(dir)
                else:
                    # Copy files, adding them to the list of output files.
                    for data in f[1]:
                        data = convert_path(data)
                        (out, _) = self.copy_file(data, dir)
                        self.outfiles.append(out)

        for scriptname in scripts.keys():
            pathname = scripts[scriptname]['dest']
            dir = convert_path(pathname)
            dir = os.path.dirname(dir)
            dir = change_root(self.root, dir)
            self.mkpath(dir)

            data = os.path.join('dist', scriptname)
            (out, _) = self.copy_file(data, dir, preserve_mode=True)
            self.outfiles.append(out)

        if self.record:
            outputs = self.get_outputs()
            if self.root:               # strip any package prefix
                root_len = len(self.root)
                for counter in xrange(len(outputs)):
                    outputs[counter] = outputs[counter][root_len:]
            self.execute(write_file,
                         (self.record, outputs),
                         "writing list of installed files to '%s'" %
                         self.record)


    def get_inputs(self):
        return self.data_files or []

    def get_outputs(self):
        return self.outfiles


def get_data_files(rootdir):
    # automatically build list of (dir, [file1, file2, ...],)
    # for all files under src/root/ (or provided rootdir)
    results = []
    for root, dirs, files in os.walk(rootdir):
        if len(files) > 0:
            dirname = os.path.relpath(root, rootdir)
            flist = [os.path.join(root, f) for f in files]
            results.append(("/%s" % dirname, flist))

    return results

data_files = get_data_files("root")
data_files.append('cb-event-forwarder.spec')
data_files.append('scripts/cb-event-forwarder')
scripts = {
    'cb-event-forwarder': {
        'spec': 'cb-event-forwarder.spec',
        'dest': '/usr/share/cb/integrations/event-forwarder/cb-event-forwarder'
    }
}

setup(
    name='python-cb-event-forwarder',
    version='2.0',
    packages=['cbeventforwarder'],
    url='https://github.com/carbonblack/cb-event-forwarder',
    license='MIT',
    author='Bit9 + Carbon Black Developer Network',
    author_email='dev-support@bit9.com',
    description=
        'Connector for evaluating Yara signatures against the Carbon Black modulestore',
    data_files=data_files,
    classifiers=[
        'Development Status :: 4 - Beta',

        # Indicate who your project is intended for
        'Intended Audience :: Developers',

        # Pick your license as you wish (should match "license" above)
         'License :: OSI Approved :: MIT License',

        # Specify the Python versions you support here. In particular, ensure
        # that you indicate whether you support Python 2, Python 3 or both.
        'Programming Language :: Python :: 2',
        'Programming Language :: Python :: 2.6',
        'Programming Language :: Python :: 2.7',
    ],
    keywords='carbonblack bit9',
    cmdclass={'install_cb': install_cb, 'bdist_binaryrpm': bdist_binaryrpm}
)
