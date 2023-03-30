#!/usr/bin/env bash

usage() {
  cat <<-EOF
  Usage: $(basename $0) [options] <label>
  Options:
    -l, --label <label>  Label used to calculate the DNS-ACCOUNT-01 value with.
    -h, --help           Display this help message
EOF
}

while [[ $# -gt 0 ]]; do
  key="$1"

  case $key in
    -l|--label)
      label="$2"
      shift
      shift
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      printf "Unknown option: $1\n\n"
      usage
      exit 1
      ;;
  esac
done

if [ -z "$label" ]; then
  label="https://example.com/acme/acct/ExampleAccount"
fi

printf "$label" | shasum -a 256 | xxd -r -p | head -c 10 | base32
