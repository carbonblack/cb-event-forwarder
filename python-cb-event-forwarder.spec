%define name python-cb-event-forwarder
%define version 2.0
%define unmangled_version 2.0
%define release 1
%global _enable_debug_package 0
%global debug_package %{nil}
%global __os_install_post /usr/lib/rpm/brp-compress %{nil}

Summary: Carbon Black event forwarder
Name: %{name}
Version: %{version}
Release: %{release}
Source0: %{name}-%{unmangled_version}.tar.gz
License: MIT
Group: Development/Libraries
BuildRoot: %{_tmppath}/%{name}-%{version}-%{release}-buildroot
Prefix: %{_prefix}
BuildArch: x86_64
Vendor: Carbon Black
Url: http://www.carbonblack.com/

%description
UNKNOWN

%prep
%setup -n %{name}-%{unmangled_version}

%build
pyinstaller cb-lastline-connector.spec

%install
python setup.py install_cb --root=$RPM_BUILD_ROOT --record=INSTALLED_FILES

%clean
rm -rf $RPM_BUILD_ROOT

%post
#!/bin/sh

chkconfig --add cb-event-forwarder

chkconfig --level 345 cb-event-forwarder on

# not auto-starting because conf needs to be updated
#/etc/init.d/cb-event-forwarder start


%preun
#!/bin/sh

/etc/init.d/cb-event-forwarder stop

chkconfig --del cb-event-forwarder

%files -f INSTALLED_FILES
%defattr(-,root,root)
