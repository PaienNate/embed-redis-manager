<template>
  <span>
    <el-input
      :value='file'
      clearable
      @clear='clearFile'
      @focus='focus'
      :placeholder='placeholder'>
      <template slot="append">
        <el-button @click='showFileSelector'>...</el-button>
      </template>
    </el-input>
    <input type="file" ref="fileInput" style="display:none" @change="onFileChange">
  </span>
</template>

<script type="text/javascript">
export default {
  props: {
    file: { default: '' },
    bookmark: { default: '' },
    placeholder: { default: 'Select File' },
  },
  methods: {
    clearFile() {
      this.$emit('update:file', '');
      this.$emit('update:bookmark', '');
    },
    focus(e) {
      // edit is forbidden, input blur
      e.target.blur();
    },
    showFileSelector() {
      this.$refs.fileInput.click();
    },
    onFileChange(event) {
      const file = event.target.files && event.target.files[0];
      if (!file) {
        return;
      }
      // browser cannot expose a local absolute path; emit the file name as a handle
      this.$emit('update:file', file.name);
      this.$emit('update:bookmark', '');
    },
  },
};
</script>
