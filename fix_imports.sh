#!/bin/sh
cd /data/see-refactor
i=0
while [ $i -lt 12 ]; do
  i=$((i+1))
  out=$(go vet ./internal/app 2>&1)
  echo "$out" | head -5
  # "file:line:col: \"pkg\" imported and not used"
  n=$(echo "$out" | grep -c 'imported and not used')
  if [ "$n" -eq 0 ]; then
    break
  fi
  echo "$out" | grep 'imported and not used' | while IFS=: read -r file line rest; do
    echo "dropping $file:$line"
    sed -i "${line}d" "$file"
  done
done
gofmt -w internal/app
go vet ./... 2>&1 | head -20
echo VET_DONE