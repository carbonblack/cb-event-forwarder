%define name cb-event-forwarder
%global _enable_debug_package 0
%global debug_package %{nil}
%global __os_install_post /usr/lib/rpm/brp-compress %{nil}

Summary: Carbon Black event forwarder
Name: %{name}
Version: %{version}
Release: %{release}
Source0: %{name}-%{version}.tar.gz
License: MIT
Group: Development/Libraries
BuildRoot: %{_tmppath}/%{name}-%{version}-%{release}-buildroot
Prefix: %{_prefix}
BuildArch: x86_64
Vendor: Carbon Black
Url: http://www.carbonblack.com/

%description
Carbon Black Event Forwarder is a standalone service that will listen on the Carbon Black enterprise bus and export
events (both watchlist/feed hits as well as raw endpoint events, if configured) in a normalized JSON or LEEF format.
The events can be saved to a file, delivered to a network service or archived automatically to an Amazon AWS S3 bucket.
These events can be consumed by any external system that accepts JSON or LEEF, including Splunk and IBM QRadar.

%prep
%setup -n %{name}-%{version}

%build
export GOPATH=$PWD
cd ./src/github.com/carbonblack/cb-event-forwarder && make rpmbuild

%install
export GOPATH=$PWD
cd ./src/github.com/carbonblack/cb-event-forwarder && make rpminstall

%clean
rm -rf $RPM_BUILD_ROOT

%post
#!/bin/sh
mkdir -p /var/log/cb/integrations/cb-event-forwarder
mkdir -p /var/cb/data

%preun
#!/bin/sh


%files -f MANIFEST
%defattr(-,root,root)
%config(noreplace) /etc/cb/integrations/event-forwarder/cb-event-forwarder.conf
