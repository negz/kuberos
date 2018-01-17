<template>
  <div id="kuberos">
    <b-alert v-if="error" show variant="danger">
      <p>Could not connect to authentication API: {{error.response.status}} {{error.response.statusText}}</p>
    </b-alert>
    <b-container v-else fluid>
      <b-row><br /></b-row>
      <b-row>
        <b-col></b-col>
        <b-col md="10">
          <b-jumbotron>
            <template slot="header">Kuberos</template>
            <template slot="lead">
              Save <a :href="templateURL()">this file</a> as <code>~/.kube/config</code>
              to enable OIDC based <code>kubectl</code> authentication.
            </template>
          </b-jumbotron>
        </b-col>
        <b-col></b-col>
      </b-row>

      <b-row>
        <b-col></b-col>
        <b-col md="10">
          <h3>Running <code>kubectl</code></h3>
          <p>
            Once you've saved the above <code>~/.kube/config</code> file you should be
            able to run <code>kubectl</code>:
          </p>
          <pre v-highlightjs><code class="bash"># These are examples. Your context and cluster names will likely differ.

$ kubectl config get-contexts
CURRENT   NAME          CLUSTER                    AUTHINFO   NAMESPACE
          experimental  experimental.example.org   kuberos
          prod          prod.example.org           kuberos

$ kubectl --context experimental get namespaces
NAME          STATUS    AGE
default       Active    83d
experimental  Active    15d

$ kubectl --context experimental -n experimental get pods
NAME                                       READY     STATUS             RESTARTS   AGE
kuberos-4074452424-06m0b                   1/1       Running            1          6d</code></pre>
          <h3>Authenticate Manually</h3>
          <p>
            If you want to maintain your existing <code>~/.kube/config</code>
            file you can run the following to add your user:
          </p>
          <pre v-highlightjs="snippetSetCreds()"><code class="bash"></code></pre>
        </b-col>
        <b-col></b-col>
      </b-row>
    </b-container>
  </div>
</template>

<script>
export default {
  name: "kuberos",
  metaInfo: {
    title: "Kubernetes Authentication",
    htmlAttrs: {
      lang: "en"
    }
  },
  data: function() {
    return {
      error: null,
      kubecfg: {}
    };
  },
  methods: {
    templateURL: function() {
      return "/kubecfg.yaml?" + $.param(this.kubecfg);
    },
    snippetSetCreds: function() {
      return (
        "# Add your user to kubectl\n" +
        'kubectl config set-credentials "' +
        this.kubecfg.email +
        '" \\\n' +
        "  --auth-provider=oidc \\\n" +
        '  --auth-provider-arg=client-id="' +
        this.kubecfg.clientID +
        '" \\\n' +
        '  --auth-provider-arg=client-secret="' +
        this.kubecfg.clientSecret +
        '" \\\n' +
        '  --auth-provider-arg=id-token="' +
        this.kubecfg.idToken +
        '" \\\n' +
        '  --auth-provider-arg=refresh-token="' +
        this.kubecfg.refreshToken +
        '" \\\n' +
        '  --auth-provider-arg=idp-issuer-url="' +
        this.kubecfg.issuer +
        '"\n\n' +
        "# Associate your user with an existing cluster\n" +
        "export CLUSTER=coolcluster\n" +
        "export CONTEXT=coolcontext\n" +
        'kubectl config set-context ${CONTEXT} --cluster ${CLUSTER} --user="' +
        this.kubecfg.email +
        '"'
      );
    }
  },
  created: function() {
    var q = decodeURI(location.search.substr(1))
      .replace(/"/g, '\\"')
      .replace(/&/g, '","')
      .replace(/=/g, '":"');
    var query = "";
    if (q != "") {
      query = JSON.parse('{"' + q + '"}');
    }
    var url = "/kubecfg?" + $.param(query);

    var _this = this;
    this.axios
      .get(url)
      .then(function(response) {
        _this.kubecfg = response.data;
      })
      .catch(function(error) {
        _this.error = error;
      });
  }
};
</script>
