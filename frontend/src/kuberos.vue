<template>
  <div id="kuberos">
    <div v-if="error" class="alert alert-danger">
      <p>Could not connect to authentication API: {{error.response.status}} {{error.response.statusText}}</p>
    </div>
    <div v-else>
      <p>Run this command to add your user to kubectl:</p>
      <pre>
      $ kubectl config set-credentials "{{kubecfg.email}}" \
        --auth-provider=oidc \
        --auth-provider-arg=client-id="{{kubecfg.clientID}}" \
        --auth-provider-arg=client-secret="{{kubecfg.clientSecret}}" \
        --auth-provider-arg=id-token="{{kubecfg.idToken}}" \
        --auth-provider-arg=refresh-token="{{kubecfg.refreshToken}}" \
        --auth-provider-arg=idp-issuer-url="{{kubecfg.issuer}}"
      </pre>
      Then this one to associate your user with an existing cluster:
      <pre>
      $ kubectl config set-context mycluster \
        --cluster mycluster \
        --user="{{kubecfg.email}}"
      </pre>
      <p>Or just save <a :href="templateURL()">this file</a> as <code>~/.kube/config</code>.</p>
    </div>
  </div>
</template>

<script>
export default {
  name: 'kuberos',
  data: function() {
    return {
      error: null,
      kubecfg: {
        email: "",
        clientID: "",
        clientSecret: "",
        idToken: "",
        refreshToken: "",
        issuer: ""
      }
    }
  },
  methods: {
    'templateURL': function() {
      return "/kubecfg.yaml?" + $.param(this.kubecfg);
    }
  },
  created: function() {
    var q = decodeURI(location.search.substr(1))
      .replace(/"/g, '\\"')
      .replace(/&/g, '","')
      .replace(/=/g,'":"');
    var query = "";
    if (q != "") {
      query = JSON.parse('{"' + q + '"}');
    }
    var url = "/kubecfg?" + $.param(query);

    var _this = this;
    this.axios.get(url).then(function(response) {
      _this.kubecfg = response.data;
    }).catch(function(error) {
      _this.error = error;
    })
  }
}
</script>
