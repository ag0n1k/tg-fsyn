VERSION=$(cat version)
docker manifest inspect ag0n1k/tg-fsync:v$VERSION > /dev/null 2>&1 && VERSION_NEW=$(echo ${VERSION} | awk -F. -v OFS=. '{$NF += 1 ; print}') || VERSION_NEW=$VERSION

echo -n $VERSION_NEW > version

docker build -t ag0n1k/tg-fsync:v$VERSION_NEW .
docker push ag0n1k/tg-fsync:v$VERSION_NEW
