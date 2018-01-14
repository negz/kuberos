import axios from 'axios'

import Vue from 'vue'
import VueAxios from 'vue-axios'
import Meta from 'vue-meta'
import VueHighlightJS from 'vue-highlightjs'
import BootstrapVue from 'bootstrap-vue'

import Kuberos from './kuberos.vue'

import 'bootswatch/dist/darkly/bootstrap.css'
import 'bootstrap-vue/dist/bootstrap-vue.css'
import 'highlight.js/styles/github.css'

Vue.use(VueAxios, axios)
Vue.use(Meta);
Vue.use(BootstrapVue)
Vue.use(VueHighlightJS)

new Vue({
  el: '#app',
  render: h => h(Kuberos),
  template: '<Kuberos/>',
  components: { Kuberos }
})
