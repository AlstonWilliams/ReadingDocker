# !/bin/bash

sudo service docker start && make build && make binary && sudo service docker stop
