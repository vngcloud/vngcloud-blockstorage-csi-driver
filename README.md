# VngCloud Blockstorage CSI Driver

**DEV** - [![Build image on DEV branch](https://github.com/vngcloud/vngcloud-blockstorage-csi-driver/actions/workflows/build_dev.yml/badge.svg)](https://github.com/vngcloud/vngcloud-blockstorage-csi-driver/actions/workflows/build_dev.yml)

**PROD** - [![Build image on MAIN branch](https://github.com/vngcloud/vngcloud-blockstorage-csi-driver/actions/workflows/build_prod.yml/badge.svg)](https://github.com/vngcloud/vngcloud-blockstorage-csi-driver/actions/workflows/build_prod.yml) 

**RELEASE** - [![Release vngcloud-blockstorage-csi-driver project](https://github.com/vngcloud/vngcloud-blockstorage-csi-driver/actions/workflows/release.yml/badge.svg)](https://github.com/vngcloud/vngcloud-blockstorage-csi-driver/actions/workflows/release.yml)

<hr>

# Contributing
## Development
- For the **development** purpose, this project uses the `dev` branch to deploy and build the image. Followings are the steps to build and push new images to VngCloud Registry:
  ```bash
  # Make sure you are on the `dev` branch
  git add .
  git commit -am "[build] This is a really spectacular feature for this plugin"
  git push
  ```
  
## Production
- The branch `main` is used to trigger the release workflow via GitHub actions. To deploy your new code as the latest stable version, follow these steps:
  ```bash
  # Make sure you are on the `main` branch
  git add .
  git commit -am "[release] This is a really spectacular feature for this plugin"
  git tag -am "[release] This is a really spectacular feature for this plugin" v1.0.0
  git push --tags
  ``` 
  - The above steps also trigger to create a new **Release** in GitHub.