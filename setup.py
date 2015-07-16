#!/usr/bin/env python

import os
import sys

sys.path.insert(0, "src/")

from cbeventforwarder import version
from distutils.core import setup


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

data_files = get_data_files("src/root")

setup(
    name='cb-event-forwarder',
    version=version.__version__,
    url='http://www.carbonblack.com/',
    license='MIT',
    author='Carbon Black',
    author_email='support@carbonblack.com',
    description='Carbon Black Event Bus Forwarder',
    long_description=__doc__,
    packages=['cbeventforwarder', ],
    data_files=data_files,
    package_dir={'cbeventforwarder': 'src/cbeventforwarder'},
    package_data={'cbeventforwarder': ['lib/*']},
    options={'bdist_rpm': {'post_install': 'post_install', 'pre_uninstall': 'pre_uninstall'}},
    platforms='any',
    classifiers=[
        'Environment :: Environment',
        'Intended Audience :: Developers',
        'License :: MIT',
        'Operating System :: OS Independent',
        'Programming Language :: Python',
        'Topic :: Software Development :: Libraries :: Python Modules'
    ]
)
