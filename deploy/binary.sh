echo "-----------Installing_dependencies---------------"
yum -y install curl gcc gcc-c++ kernel-devel make ca-certificates tar git jq python3 nano wget

echo "--------------install_golang---------------------------"
curl https://dl.google.com/go/go1.16.4.linux-amd64.tar.gz --output go.tar.gz
tar -C /usr/local -xzf go.tar.gz
PATH="/usr/local/go/bin:$PATH"
GOPATH=/go
PATH=$PATH:$GOPATH/bin

echo "----------------cloning_repository-------------------"
ROOT_DIR=~
git clone -b load-testing https://github.com/sunnyk56/tm-load-test.git

echo "----------------------building_binary---------------"
cd $ROOT_DIR/tm-load-test

make

cp $ROOT_DIR/tm-load-test/build/tm-load-test /usr/bin/tm-load-test

cd $ROOT_DIR




