<template>
  <!-- setting dialog -->
  <el-dialog :title="$t('message.settings')" :visible.sync="visible" custom-class="setting-main-dialog">
    <el-form label-position="top" size="mini">

      <el-card :header="$t('message.ui_settings')" class="setting-card">
        <el-row :gutter="10" justify="space-between" type="flex" class="setting-row">
          <el-col :sm="12" :lg="5">
            <!-- theme select-->
            <el-form-item :label="$t('message.theme_select')">
              <el-select v-model='themeMode' @change="changeTheme">
                <el-option
                  v-for="(label, theme) in themeList"
                  :key="theme"
                  :value="theme"
                  :label="label">
                </el-option>
              </el-select>
            </el-form-item>
          </el-col>
          <el-col :sm="12" :lg="7">
            <!-- language select -->
            <el-form-item :label="$t('message.select_lang')">
              <LanguageSelector></LanguageSelector>
            </el-form-item>
          </el-col>
          <el-col :sm="12" :lg="5">
            <!-- zoom page -->
            <el-form-item :label="$t('message.page_zoom')">
              <el-input-number
                size="mini"
                placeholder='1.0'
                :min=0.5
                :max=2.0
                :step=0.1
                :precision=1
                @change='changeZoom'
                v-model='form.zoomFactor'>
              </el-input-number>
            </el-form-item>
          </el-col>
          <el-col :sm="12" :lg="7">
            <!-- font-family -->
            <el-form-item :label="$t('message.font_family')">
              <span slot="label">
                {{ $t('message.font_family') }}
                <el-popover
                  placement="top-start"
                  :title="$t('message.font_faq_title')"
                  trigger="hover">
                  <i slot="reference" class="el-icon-question"></i>
                  <p v-html="$t('message.font_faq')"></p>
                </el-popover>
                <i v-if="loadingFonts" class="el-icon-loading"></i>
              </span>
              <!-- font-family select -->
              <el-select v-model="form.fontFamily" @visible-change="getAllFonts" allow-create default-first-option
                         filterable multiple class="setting-font-select">
                <el-option
                  v-for="(font, index) in allFonts"
                  :key="index"
                  :label="font"
                  :value="font">
                  <!-- for better performance, do not display font-family -->
                  <!-- :style="{'font-family': font}"> -->
                </el-option>
              </el-select>
            </el-form-item>
          </el-col>
        </el-row>
      </el-card>

      <el-card :header="$t('message.common_settings')" class="setting-card">
        <el-row :gutter="20" justify="space-between" type="flex" class="setting-row">
          <el-col :sm="12" :lg="12">
            <!-- keys per loading -->
            <el-form-item>
              <el-input-number
                size="mini"
                placeholder='500'
                :min=10
                :max=20000
                :step=50
                v-model='form.keysPageSize'>
              </el-input-number>&nbsp;
              <!-- load all switch -->
              <!-- <el-switch v-model='form.showLoadAllKeys'></el-switch>
              {{ $t('message.show_load_all_keys') }} -->

              <span slot="label">
                {{ $t('message.keys_per_loading') }}
                <el-popover
                  :content="$t('message.keys_per_loading_tip')"
                  placement="top-start"
                  trigger="hover">
                  <i slot="reference" class="el-icon-question"></i>
                </el-popover>
              </span>
            </el-form-item>
          </el-col>
        </el-row>
      </el-card>

      <el-card class="setting-card">
        <div slot="header">
          {{$t('message.pre_version')}}
          <el-tag type="info">{{ appVersion }}</el-tag>
        </div>
        <div class="current-version">
          <a href="###" @click.stop.prevent="showHotkeys">{{ $t('message.hotkey') }}</a>
          <a href="###" @click.stop.prevent="clearCache">{{ $t('message.clear_cache') }}</a>
        </div>
        <div class="acknowledgement">
          {{ $t('message.acknowledgements') }}:
          <a href="https://github.com/qishibo/AnotherRedisDesktopManager" target="_blank" rel="noopener noreferrer">Another Redis Desktop Manager</a>
        </div>
      </el-card>
    </el-form>

    <div slot="footer" class="dialog-footer">
      <el-button @click="visible = false">{{ $t('el.messagebox.cancel') }}</el-button>
      <el-button type="primary" @click="saveSettings">{{ $t('el.messagebox.confirm') }}</el-button>
    </div>

  </el-dialog>
</template>

<script type="text/javascript">
import storage from '@/storage.js';
import LanguageSelector from '@/components/LanguageSelector';

export default {
  data() {
    return {
      visible: false,
      form: {
        fontFamily: '',
        zoomFactor: 1.0,
        keysPageSize: 500,
        showLoadAllKeys: false,
      },
      appVersion: (window.__REDISUI__ && window.__REDISUI__.version) || '',
      // electronVersion: process.versions.electron,
      allFonts: [],
      loadingFonts: false,
      themeMode: 'system',
    };
  },
  components: { LanguageSelector },
  computed: {
    // themeList in computed to activate i18n
    themeList() {
      return {
        system: this.$t('message.theme_system'),
        light: this.$t('message.theme_light'),
        dark: this.$t('message.theme_dark')
      };
    },
  },
  methods: {
    show() {
      this.visible = true;
    },
    restoreSettings() {
      const settings = storage.getSetting();
      this.form = { ...this.form, ...settings };

      // theme
      let theme = localStorage.theme;
      if (!Object.keys(this.themeList).includes(theme)) {
        theme = 'system';
      }

      this.themeMode = theme;
    },
    saveSettings() {
      storage.saveSettings(this.form);

      this.visible = false;
      this.$bus.$emit('reloadSettings', Object.assign({}, this.form));
    },
    changeTheme() {
      localStorage.theme = this.themeMode;
      globalChangeTheme(this.themeMode);
    },
    changeZoom() {
      // window/zoom factor is an Electron concept, noop in a browser build
    },
    bindGetAllFonts() {
      // system font enumeration is provided by Electron's ipcRenderer in the
      // desktop build; in the browser use a sensible static default list.
      this.allFonts = [
        'Default Initial',
        'serif',
        'sans-serif',
        'monospace',
        'Menlo',
        'Consolas',
        'Monaco',
        'Courier New',
        'Ubuntu Mono',
      ];
      this.loadingFonts = false;
    },
    getAllFonts() {
      if (this.allFonts.length === 0) {
        this.loadingFonts = true;
        this.bindGetAllFonts();
      }
    },
    clearCache() {
      this.$confirm(this.$t('message.clear_cache_tip')).then(() => {
        localStorage.clear();
        this.$message.success(this.$t('message.delete_success'));
        window.location.reload();
      }).catch((e) => {
      });
    },
    showHotkeys() {
      this.$parent.$refs.hotKeysDialog.show();
    },
  },
  mounted() {
    this.restoreSettings();
    this.bindGetAllFonts();
  },
};
</script>

<style type="text/css">
.setting-main-dialog {
  width: 80%;
  max-width: 900px;
  margin-top: 7vh !important;
}

.dark-mode .el-upload-dragger {
  background: inherit;
}

.setting-main-dialog .current-version a {
  color: grey;
  font-size: 95%;
}

.setting-main-dialog .acknowledgement {
  margin-top: 8px;
  color: grey;
  font-size: 95%;
}
.setting-main-dialog .acknowledgement a {
  color: grey;
  text-decoration: underline;
}

.setting-main-dialog .setting-card {
  margin-bottom: 8px;
}
.setting-main-dialog .setting-card .el-card__header {
  font-size: 105%;
  font-weight: bold;
}

.setting-main-dialog .setting-card .setting-row {
  flex-wrap: wrap;
}

/* add height: fix el-select jitter when multiple*/
.setting-main-dialog .setting-card .setting-row .setting-font-select .el-select__tags .el-tag {
  height: 21px;
  max-width: 98%;
}

/*label style inside el-select multiple*/
.setting-main-dialog .setting-card .setting-row .setting-font-select .el-select__tags .el-tag .el-select__tags-text {
  display: inline-block;
  max-width: 90%;
  overflow: hidden;
  text-overflow: ellipsis;
}
/*fix close icon vertical align*/
.setting-main-dialog .setting-card .setting-row .setting-font-select .el-select__tags .el-tag .el-tag__close {
  vertical-align: super;
}
</style>
