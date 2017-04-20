import axios from 'axios'

import Vue from 'vue'
import VueAxios from 'vue-axios'
import BootstrapVue from 'bootstrap-vue'

import Kuberos from './kuberos.vue'

import 'style-loader!css-loader!bootswatch/darkly/bootstrap.css'

Vue.use(VueAxios, axios)
Vue.use(BootstrapVue)

new Vue({
  el: '#app',
  render: h => h(Kuberos),
  template: '<Kuberos/>',
  components: { Kuberos }
})
