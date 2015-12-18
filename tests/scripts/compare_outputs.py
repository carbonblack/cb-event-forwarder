#!/usr/bin/env python

import json
import os


def compare_json(gold_root, other_root, path, errors):
    if type(gold_root) != type(other_root):
        errors.append("%s: type mismatch. expected type(%s), got type(%s)" % (path, type(gold_root).__name__,
                                                                              type(other_root).__name__))

    if type(gold_root) == dict:
        for key in gold_root.keys():
            newpath = "%s.%s" % (path, key)
            if key not in other_root:
                errors.append("%s: not found. should have value %s" % (newpath, gold_root[key]))
            else:
                compare_json(gold_root[key], other_root[key], newpath, errors)

        for key in other_root.keys():
            newpath = "%s.%s" % (path, key)
            if key not in gold_root:
                errors.append("%s: now has value %s in the new output" % (newpath, other_root[key]))
    elif type(gold_root) == list:
        if len(gold_root) != len(other_root):
            errors.append("%s: length mismatch. expected %d, got %d" % (path, len(gold_root), len(other_root)))
            return
        for i in range(len(gold_root)):
            newpath = "%s[%d]" % (path, i)
            compare_json(gold_root[i], other_root[i], newpath, errors)
    else:
        if gold_root != other_root:
            errors.append("%s: (%s) expected %s, got %s" % (path, type(gold_root).__name__, gold_root, other_root))


def compare_files(fn, gold, other):
    outerr = []

    if len(gold) > len(other):
        raise Exception("ERROR: gold output has more lines")
    elif len(other) > len(gold):
        raise Exception("ERROR: gold output has less lines")

    for i in range(0, len(gold)):
        inobj = json.loads(gold[i])
        testobj = json.loads(other[i])

        errors = []
        path = ""
        compare_json(inobj, testobj, path, errors)

        if errors:
            outerr.append("file %s line %d:\n  %s" % (fn, i, "\n  ".join(errors)))

    return "\n".join(outerr)


def process_dirs(gold_output, go_output):
    for format in ["json", "protobuf"]:
        if not os.path.isdir(os.path.join(gold_output, format)):
            raise Exception("FATAL: no %s directory found in python output directory" % format)

        if not os.path.isdir(os.path.join(go_output, format)):
            raise Exception("FATAL: no %s directory found in go output directory" % format)

        format_path = os.path.join(gold_output, format)
        for routing_key in [x for x in os.listdir(format_path) if os.path.isdir(os.path.join(format_path, x))]:
            print "Processing %s/%s..." % (format, routing_key)
            go_path = os.path.join(go_output, format, routing_key)
            python_path = os.path.join(gold_output, format, routing_key)

            for fn in [x for x in os.listdir(python_path) if os.path.isfile(os.path.join(python_path, x))]:
                go_fn_path = os.path.join(go_path, fn)
                python_fn_path = os.path.join(python_path, fn)
                if not os.path.isfile(go_fn_path):
                    print "ERROR: file %s not found in the Go output directory" % go_fn_path
                    continue

                python_data = open(python_fn_path, 'rb').readlines()
                go_data = open(go_fn_path, 'rb').readlines()

                try:
                    errors = compare_files(go_fn_path, python_data, go_data)
                except Exception as e:
                    print str(e) + ": %s" % go_fn_path
                else:
                    if errors:
                        print errors


if __name__ == '__main__':
    process_dirs("../gold_output", "../go_output")