#!/usr/bin/env python

import sys
import json
import logging
import os
import re
import traceback

logging.basicConfig(filename='middleware.log',
                    level=logging.DEBUG,
                    format='%(asctime)s %(message)s')

#
# Didn't know what directory the log file was created.  This is useful:
# logging.debug("Current Working Directory: {}".format(os.getcwd()))
#

response_json = '{"feed_id": 59, "timestamp": 1380773388, "create_time": 1380773388, "link": "https://my.wedgie.org", ' \
                '"id": "randommd5", "title": "This is a test title", "has_query": false, ' \
                '"iocs": {"md5": ["A4F6DF0E33E644E802C8798ED94D80EA"]}, "is_ignored": false, ' \
                '"feed_name": "testquery2", "score": 1}'

#
# NOTE:
# You can test this script by using:
# curl https://192.168.1.42/api/v1/feed/15/report/testreportid --proxy http://localhost:8500/ -k
#

def build_response(feed_id, report_id):
    response = json.loads(response_json)
    response['feed_id'] = feed_id
    response['id'] = report_id

    return json.dumps(response)

def main():
    #
    # This script works for input until EOF is reached
    #
    data = sys.stdin.readlines()
    payload = data[0]
    # logging.debug(payload)
    payload_dict = json.loads(payload)

    try:
        pattern = re.compile('/api/v1/feed/(\d*)/report/(\w*)')
        match_object = pattern.match(payload_dict['request']['path'])
        if match_object:
            #
            # Get feed id from regex match group
            #
            feed_id = match_object.group(1)

            #
            # Get report id from regex match group
            #
            report_id = match_object.group(2)

            #
            # Send back a 200 with the correct feed_id and report_id
            #
            payload_dict['response']['status'] = 200
            payload_dict['response']['body'] = build_response(feed_id, report_id)

            print(json.dumps(payload_dict))

        else:
            logging.debug("Error: path did not match any regex")
            payload_dict['response']['status'] = 500
            payload_dict['response']['body'] = ''
            print payload_dict

    except:
        logging.debug(traceback.format_exc())
        payload_dict['response']['status'] = 500
        payload_dict['response']['body'] = ''
        print payload_dict


if __name__ == "__main__":
    main()
