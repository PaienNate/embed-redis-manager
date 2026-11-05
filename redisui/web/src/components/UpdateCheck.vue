<template>
</template>

<script type="text/javascript">
export default {
  data() {
    return {
      manual: false,
      updateChecking: false,
      downloadProcessShow: false,
    };
  },
  created() {
    this.$bus.$on('update-check', (manual = false) => {
      this.manual = manual;
      // auto update is provided by Electron in the desktop build; not available
      // in the browser/embedded build.
      manual && this.$notify.info({
        title: this.$t('message.update_not_available'),
        duration: 2000,
      });
    });
  },
  methods: {},
};
</script>

<style type="text/css">
  .download-progress-container .el-progress {
    width: 280px;
  }
</style>
